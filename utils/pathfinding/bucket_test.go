package pathfinding

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBucketQueueReturnsDistancesInAscendingOrder(t *testing.T) {
	var queue bucketQueue
	queue.push(bucketEntry{index: 90, distance: 513})
	queue.push(bucketEntry{index: 20, distance: 2})
	queue.push(bucketEntry{index: 70, distance: 257})
	queue.push(bucketEntry{index: 10, distance: 1})
	queue.push(bucketEntry{index: 21, distance: 2})

	want := []bucketEntry{
		{index: 10, distance: 1},
		{index: 20, distance: 2},
		{index: 21, distance: 2},
		{index: 70, distance: 257},
		{index: 90, distance: 513},
	}
	for _, expected := range want {
		actual, ok := queue.pop()
		require.True(t, ok)
		assert.Equal(t, expected, actual)
	}
	_, ok := queue.pop()
	assert.False(t, ok)
}

func TestBucketQueueResetRetainsBucketStorage(t *testing.T) {
	var queue bucketQueue
	queue.ensure(600)
	fill := func() {
		for index := int64(0); index < 600; index++ {
			queue.push(bucketEntry{index: index, distance: Distance(index % 300)})
		}
	}
	fill()
	storage := cap(queue.storage)
	links := cap(queue.links)
	require.Greater(t, storage, 0)
	require.Greater(t, links, 0)

	queue.reset()
	assert.Zero(t, queue.pending)
	assert.Equal(t, storage, cap(queue.storage))
	assert.Equal(t, links, cap(queue.links))

	allocations := testing.AllocsPerRun(20, func() {
		fill()
		for {
			if _, ok := queue.pop(); !ok {
				break
			}
		}
		queue.reset()
	})
	assert.Zero(t, allocations)
}

func TestBucketQueueEnsureCoversPartialChunks(t *testing.T) {
	const entries = 5000
	buckets := entries
	if buckets > distanceBucketCount {
		buckets = distanceBucketCount
	}
	counts := make([]int, buckets)
	left := entries
	fillBucketCounts(buckets, left, counts)

	var queue bucketQueue
	queue.ensure(entries)
	reserved := cap(queue.links)
	index := int64(0)
	for bucket, count := range counts {
		for n := 0; n < count; n++ {
			queue.push(bucketEntry{index: index, distance: Distance(bucket)})
			index++
		}
	}
	require.LessOrEqual(t, queue.used, reserved)
	require.Equal(t, entries, queue.pending)

	got := 0
	var last Distance = -1
	for {
		entry, ok := queue.pop()
		if !ok {
			break
		}
		require.GreaterOrEqual(t, entry.distance, last)
		last = entry.distance
		got++
	}
	assert.Equal(t, entries, got)
}

func fillBucketCounts(buckets int, left int, counts []int) {
	for index := 0; index < buckets && left > 0; index++ {
		counts[index] = 1
		left--
	}
	for left > 0 {
		for index := 0; index < buckets && left > 0; index++ {
			add := bucketChunkCap
			if add > left {
				add = left
			}
			counts[index] += add
			left -= add
		}
	}
}

func TestBucketQueuePopsInStableDistanceOrder(t *testing.T) {
	var queue bucketQueue
	pushed := make([]bucketEntry, 0, 300)
	for index := 0; index < 300; index++ {
		entry := bucketEntry{
			index:    int64(index),
			distance: Distance((index*17 + index/7) % 400),
		}
		queue.push(entry)
		pushed = append(pushed, entry)
	}
	sort.SliceStable(pushed, func(i, j int) bool {
		return pushed[i].distance < pushed[j].distance
	})

	for _, expected := range pushed {
		actual, ok := queue.pop()
		require.True(t, ok)
		assert.Equal(t, expected, actual)
	}
	_, ok := queue.pop()
	assert.False(t, ok)
}
