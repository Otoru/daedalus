package pathfinding

const (
	distanceBucketCount = int(MaxCost) + 1
	// bucketChunkCap is the number of entries stored contiguously before a
	// bucket links another chunk. Pop walks that run with a cursor, so
	// draining a distance does not slide the tail down on every removal.
	// 16 keeps the worst-case reservation near one slot per entry; a wider
	// chunk pre-pays a full run for every bucket and inflates a small grid.
	bucketChunkCap = 16
)

type bucketEntry struct {
	index    int64
	distance Distance
}

type chunkLink struct {
	next int32
	n    int32
}

// bucketQueue is Dial's queue for edge weights in [MinCost, MaxCost]. The
// MaxCost+1 circular buckets preserve insertion order among equal distances.
// A binary heap is not stable and would need an explicit composite tie-break
// in every comparison; these buckets need no such extra correctness rule.
// Entries are never decreased in place. An improvement is appended and the
// obsolete entry is rejected by the distance field when it is popped.
//
// Chunks are carved from one backing array retained across reset. A search
// therefore grows that array a handful of times, not once per appended entry.
type bucketQueue struct {
	storage []bucketEntry
	links   []chunkLink
	used    int

	head   [distanceBucketCount]int32
	headAt [distanceBucketCount]int32
	tail   [distanceBucketCount]int32

	cursor      Distance
	pending     int
	initialized bool
	primed      bool
}

func (queue *bucketQueue) reset() {
	queue.resetHeads()
	queue.used = 0
	queue.cursor = 0
	queue.pending = 0
	queue.initialized = false
	queue.primed = true
}

func (queue *bucketQueue) resetHeads() {
	for index := range queue.head {
		queue.head[index] = -1
		queue.headAt[index] = 0
		queue.tail[index] = -1
	}
}

func (queue *bucketQueue) prime() {
	if queue.primed {
		return
	}
	queue.resetHeads()
	queue.primed = true
}

// ensure reserves one chunk run large enough for every way entries can fall
// across the buckets. Each bucket wastes at most chunkCap-1 slots, and at
// most distanceBucketCount buckets are used, so the reservation is
// (entries + buckets*(chunkCap-1)) / chunkCap. A later search of the same
// size reuses it.
func (queue *bucketQueue) ensure(entries int) {
	chunks := chunksFor(entries)
	if cap(queue.links) >= chunks && cap(queue.storage) >= chunks*bucketChunkCap {
		return
	}
	queue.links = make([]chunkLink, chunks)
	queue.storage = make([]bucketEntry, chunks*bucketChunkCap)
	queue.used = 0
}

func chunksFor(entries int) int {
	if entries < 1 {
		return 1
	}
	buckets := entries
	if buckets > distanceBucketCount {
		buckets = distanceBucketCount
	}
	chunks := (entries + buckets*(bucketChunkCap-1)) / bucketChunkCap
	if chunks < 1 {
		return 1
	}
	return chunks
}

func (queue *bucketQueue) push(entry bucketEntry) {
	queue.prime()
	bucket := int(entry.distance) % distanceBucketCount
	tail := queue.tail[bucket]
	if tail < 0 || queue.links[tail].n == bucketChunkCap {
		tail = queue.addChunk(bucket)
	}
	base := int(tail) * bucketChunkCap
	queue.storage[base+int(queue.links[tail].n)] = entry
	queue.links[tail].n++
	queue.pending++
	if !queue.initialized || entry.distance < queue.cursor {
		queue.cursor = entry.distance
		queue.initialized = true
	}
}

func (queue *bucketQueue) addChunk(bucket int) int32 {
	if queue.used >= len(queue.links) {
		queue.grow()
	}
	id := int32(queue.used)
	queue.used++
	queue.links[id] = chunkLink{next: -1}
	if queue.tail[bucket] < 0 {
		queue.head[bucket] = id
		queue.headAt[bucket] = 0
	} else {
		queue.links[queue.tail[bucket]].next = id
	}
	queue.tail[bucket] = id
	return id
}

func (queue *bucketQueue) grow() {
	n := len(queue.links) * 2
	if n < 16 {
		n = 16
	}
	if n <= queue.used {
		n = queue.used + 16
	}
	links := make([]chunkLink, n)
	copy(links, queue.links)
	storage := make([]bucketEntry, n*bucketChunkCap)
	copied := queue.used * bucketChunkCap
	if copied > len(queue.storage) {
		copied = len(queue.storage)
	}
	copy(storage, queue.storage[:copied])
	queue.links = links
	queue.storage = storage
}

func (queue *bucketQueue) pop() (bucketEntry, bool) {
	queue.prime()
	for queue.pending > 0 {
		bucket := int(queue.cursor) % distanceBucketCount
		entry, ok := queue.takeMatching(bucket)
		if ok {
			queue.pending--
			if queue.pending == 0 {
				queue.initialized = false
			}
			return entry, true
		}
		queue.cursor++
	}
	queue.initialized = false
	return bucketEntry{}, false
}

// takeMatching removes the next entry of the cursor distance from a bucket.
// The head entry is the common case and advances a cursor. A later entry of
// the same distance, sitting behind a greater distance that shares the
// bucket, is removed in place so the greater distance stays for its own cursor.
func (queue *bucketQueue) takeMatching(bucket int) (bucketEntry, bool) {
	chunk := queue.head[bucket]
	at := int(queue.headAt[bucket])
	for chunk >= 0 {
		n := int(queue.links[chunk].n)
		base := int(chunk) * bucketChunkCap
		if at < n && queue.storage[base+at].distance == queue.cursor {
			entry := queue.storage[base+at]
			queue.advanceHead(bucket, chunk, at)
			return entry, true
		}
		for index := at; index < n; index++ {
			if queue.storage[base+index].distance != queue.cursor {
				continue
			}
			entry := queue.storage[base+index]
			copy(queue.storage[base+index:base+n-1], queue.storage[base+index+1:base+n])
			queue.links[chunk].n = int32(n - 1)
			return entry, true
		}
		chunk = queue.links[chunk].next
		at = 0
	}
	return bucketEntry{}, false
}

func (queue *bucketQueue) advanceHead(bucket int, chunk int32, at int) {
	at++
	if at < int(queue.links[chunk].n) {
		queue.head[bucket] = chunk
		queue.headAt[bucket] = int32(at)
		return
	}
	next := queue.links[chunk].next
	queue.head[bucket] = next
	queue.headAt[bucket] = 0
	if next < 0 {
		queue.tail[bucket] = -1
	}
}
