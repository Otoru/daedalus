package vision_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Otoru/daedalus"
	"github.com/Otoru/daedalus/utils/vision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnswerEmptyIsNonNil(t *testing.T) {
	got, err := vision.Answer(context.Background(), openGrid(2, 2), nil)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestAnswerPreservesQueryOrderAndMatchesIndependentComputes(t *testing.T) {
	grid := openGrid(9, 7)
	grid.SetTransparent(daedalus.Cell{X: 4, Y: 3}, false)
	queries := []vision.Query{
		{Origin: daedalus.Cell{X: 0, Y: 0}, Radius: 5},
		{Origin: daedalus.Cell{X: 8, Y: 6}, Radius: 4},
		{Origin: daedalus.Cell{X: 2, Y: 5}, Radius: 3},
	}
	got, err := vision.Answer(context.Background(), grid, queries)
	require.NoError(t, err)
	for index, query := range queries {
		want, computeErr := vision.Compute(context.Background(), grid, query.Origin, query.Radius)
		require.NoError(t, computeErr)
		assert.Equal(t, want.Visible, got[index].Visible, "query %d", index)
	}

	permuted := []vision.Query{queries[2], queries[0], queries[1]}
	permutedGot, err := vision.Answer(context.Background(), grid, permuted)
	require.NoError(t, err)
	assert.Equal(t, got[2].Visible, permutedGot[0].Visible)
	assert.Equal(t, got[0].Visible, permutedGot[1].Visible)
	assert.Equal(t, got[1].Visible, permutedGot[2].Visible)
}

func TestAnswerResultsOwnIndependentBuffers(t *testing.T) {
	grid := openGrid(12, 12)
	queries := []vision.Query{
		{Origin: daedalus.Cell{X: 1, Y: 1}, Radius: 5},
		{Origin: daedalus.Cell{X: 10, Y: 10}, Radius: 5},
	}
	got, err := vision.Answer(context.Background(), grid, queries)
	require.NoError(t, err)
	before := append([]byte(nil), got[1].Visible...)
	got[0].Visible[0] ^= 0xff
	assert.Equal(t, before, got[1].Visible)
}

func TestAnswerIntoValidatesBeforeGrowingOrChangingDestination(t *testing.T) {
	grid := openGrid(4, 4)
	dst := make([]vision.Field, 1, 3)
	dst[0].Width = 99
	dst[0].Height = 98
	dst[0].Visible = []byte{0xa5}
	oldVisible := dst[0].Visible
	oldCap := cap(dst)

	_, err := vision.AnswerInto(context.Background(), dst, grid, []vision.Query{
		{Origin: daedalus.Cell{X: 1, Y: 1}, Radius: vision.MaxRadius + 1},
	})
	require.ErrorIs(t, err, daedalus.ErrLimitExceeded)
	assert.Equal(t, oldCap, cap(dst))
	assert.Equal(t, uint32(99), dst[0].Width)
	assert.Same(t, &oldVisible[0], &dst[0].Visible[0])
	assert.Equal(t, []byte{0xa5}, dst[0].Visible)

	_, err = vision.AnswerInto(context.Background(), dst, grid, []vision.Query{
		{Origin: daedalus.Cell{X: 9, Y: 9}, Radius: 1},
	})
	require.ErrorIs(t, err, daedalus.ErrInvalidVisibility)
	assert.Equal(t, uint32(99), dst[0].Width)
}

func TestAnswerIntoReusesWarmFieldsWithoutAllocations(t *testing.T) {
	grid := openGrid(32, 32)
	queries := []vision.Query{
		{Origin: daedalus.Cell{X: 4, Y: 4}, Radius: 12},
		{Origin: daedalus.Cell{X: 20, Y: 20}, Radius: 12},
		{Origin: daedalus.Cell{X: 28, Y: 8}, Radius: 12},
	}
	dst := make([]vision.Field, len(queries))
	_, err := vision.AnswerInto(context.Background(), dst, grid, queries)
	require.NoError(t, err)
	allocs := testing.AllocsPerRun(100, func() {
		got, runErr := vision.AnswerInto(context.Background(), dst, grid, queries)
		if runErr != nil || len(got) != len(queries) {
			t.Fatal(runErr)
		}
	})
	assert.Zero(t, allocs)
}

func TestAnswerCancellationIsIndependentAcrossConcurrentRequests(t *testing.T) {
	grid := openGrid(64, 64)
	queries := []vision.Query{{Origin: daedalus.Cell{X: 32, Y: 32}, Radius: 63}}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	var wg sync.WaitGroup
	var canceledErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, canceledErr = vision.Answer(cancelCtx, grid, queries)
	}()
	valid, validErr := vision.Answer(context.Background(), grid, []vision.Query{
		{Origin: daedalus.Cell{X: 0, Y: 0}, Radius: 8},
	})
	wg.Wait()
	require.ErrorIs(t, canceledErr, context.Canceled)
	require.NoError(t, validErr)
	assert.Len(t, valid, 1)
	assert.True(t, valid[0].VisibleAt(daedalus.Cell{X: 0, Y: 0}))
	assert.False(t, errors.Is(validErr, context.Canceled))
}
