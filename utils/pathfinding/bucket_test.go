package pathfinding

import (
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
	for index := int64(0); index < 600; index++ {
		queue.push(bucketEntry{index: index, distance: Distance(index % 300)})
	}
	capacities := make([]int, len(queue.buckets))
	for index := range queue.buckets {
		capacities[index] = cap(queue.buckets[index])
	}

	queue.reset()
	assert.Zero(t, queue.pending)
	for index := range queue.buckets {
		assert.Empty(t, queue.buckets[index])
		assert.Equal(t, capacities[index], cap(queue.buckets[index]))
	}
}
