package pathfinding

const distanceBucketCount = int(MaxCost) + 1

type bucketEntry struct {
	index    int64
	distance Distance
}

// bucketQueue is Dial's queue for edge weights in [MinCost, MaxCost]. The
// MaxCost+1 circular buckets preserve insertion order among equal distances.
// A binary heap is not stable and would need an explicit composite tie-break
// in every comparison; these buckets need no such extra correctness rule.
// Entries are never decreased in place. An improvement is appended and the
// obsolete entry is rejected by the distance field when it is popped.
type bucketQueue struct {
	buckets     [distanceBucketCount][]bucketEntry
	cursor      Distance
	pending     int
	initialized bool
}

func (queue *bucketQueue) reset() {
	for index := range queue.buckets {
		queue.buckets[index] = queue.buckets[index][:0]
	}
	queue.cursor = 0
	queue.pending = 0
	queue.initialized = false
}

func (queue *bucketQueue) push(entry bucketEntry) {
	bucket := int(entry.distance) % distanceBucketCount
	queue.buckets[bucket] = append(queue.buckets[bucket], entry)
	queue.pending++
	if !queue.initialized || entry.distance < queue.cursor {
		queue.cursor = entry.distance
		queue.initialized = true
	}
}

func (queue *bucketQueue) pop() (bucketEntry, bool) {
	for queue.pending > 0 {
		bucketIndex := int(queue.cursor) % distanceBucketCount
		bucket := queue.buckets[bucketIndex]
		for index, entry := range bucket {
			if entry.distance != queue.cursor {
				continue
			}
			copy(bucket[index:], bucket[index+1:])
			bucket = bucket[:len(bucket)-1]
			queue.buckets[bucketIndex] = bucket
			queue.pending--
			return entry, true
		}
		queue.cursor++
	}
	queue.initialized = false
	return bucketEntry{}, false
}
