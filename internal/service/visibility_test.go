package service

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/utils/vision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStatusErrorMapsInvalidVisibility(t *testing.T) {
	t.Parallel()
	assert.Equal(t, codes.InvalidArgument, status.Code(StatusError(daedalus.ErrInvalidVisibility)))
}

func TestComputeVisibilityConvertsAndPreservesQueryOrder(t *testing.T) {
	t.Parallel()
	server := New(nil, NewAdmission(1))
	request := &daedalusv1.ComputeVisibilityRequest{
		OpacityGrid: &daedalusv1.OpacityGrid{Width: 5, Height: 1, Transparent: []byte{0x1f}},
		Queries: []*daedalusv1.VisibilityQuery{
			{Origin: &daedalusv1.Cell{X: 0, Y: 0}, Radius: 1},
			{Origin: &daedalusv1.Cell{X: 4, Y: 0}, Radius: 0},
		},
	}
	response, err := server.ComputeVisibility(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, response.GetFields(), 2)
	assert.Equal(t, []byte{0x03}, response.Fields[0].GetVisible())
	assert.Equal(t, []byte{0x10}, response.Fields[1].GetVisible())
}

func TestComputeVisibilityRejectsMalformedRequests(t *testing.T) {
	t.Parallel()
	server := New(nil, NewAdmission(1))
	cases := []struct {
		name    string
		request *daedalusv1.ComputeVisibilityRequest
		code    codes.Code
	}{
		{name: "missing grid", request: nil, code: codes.InvalidArgument},
		{name: "short bits", request: &daedalusv1.ComputeVisibilityRequest{OpacityGrid: &daedalusv1.OpacityGrid{Width: 9, Height: 1, Transparent: []byte{1}}}, code: codes.InvalidArgument},
		{name: "padding", request: &daedalusv1.ComputeVisibilityRequest{OpacityGrid: &daedalusv1.OpacityGrid{Width: 1, Height: 1, Transparent: []byte{2}}}, code: codes.InvalidArgument},
		{name: "missing query", request: &daedalusv1.ComputeVisibilityRequest{OpacityGrid: &daedalusv1.OpacityGrid{Width: 1, Height: 1, Transparent: []byte{1}}, Queries: []*daedalusv1.VisibilityQuery{nil}}, code: codes.InvalidArgument},
		{name: "opaque origin", request: &daedalusv1.ComputeVisibilityRequest{OpacityGrid: &daedalusv1.OpacityGrid{Width: 1, Height: 1, Transparent: []byte{0}}, Queries: []*daedalusv1.VisibilityQuery{{Origin: &daedalusv1.Cell{}}}}, code: codes.InvalidArgument},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			_, err := server.ComputeVisibility(context.Background(), testCase.request)
			require.Error(t, err)
			assert.Equal(t, testCase.code, status.Code(err))
		})
	}
}

func TestComputeVisibilityRejectsServiceLimitsBeforeAdmission(t *testing.T) {
	t.Parallel()
	server := New(nil, NewAdmission(1))
	server.BeginShutdown()
	request := &daedalusv1.ComputeVisibilityRequest{
		OpacityGrid: &daedalusv1.OpacityGrid{Width: maximumGridDimension + 1, Height: 1},
	}
	_, err := server.ComputeVisibility(context.Background(), request)
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestComputeVisibilityRetainsNoStateBetweenCalls(t *testing.T) {
	t.Parallel()
	server := New(nil, NewAdmission(1))
	first, err := server.ComputeVisibility(context.Background(), &daedalusv1.ComputeVisibilityRequest{
		OpacityGrid: &daedalusv1.OpacityGrid{Width: 3, Height: 1, Transparent: []byte{0x07}},
		Queries:     []*daedalusv1.VisibilityQuery{{Origin: &daedalusv1.Cell{X: 0, Y: 0}, Radius: 2}},
	})
	require.NoError(t, err)
	second, err := server.ComputeVisibility(context.Background(), &daedalusv1.ComputeVisibilityRequest{
		OpacityGrid: &daedalusv1.OpacityGrid{Width: 3, Height: 1, Transparent: []byte{0x01}},
		Queries:     []*daedalusv1.VisibilityQuery{{Origin: &daedalusv1.Cell{X: 0, Y: 0}, Radius: 0}},
	})
	require.NoError(t, err)
	assert.Equal(t, []byte{0x07}, first.Fields[0].GetVisible())
	assert.Equal(t, []byte{0x01}, second.Fields[0].GetVisible())
}

func TestComputeVisibilityMatchesSDKBitForBit(t *testing.T) {
	t.Parallel()
	grid := vision.OpacityGrid{Width: 5, Height: 3, Transparent: []byte{0xff, 0x1f}}
	queries := []vision.Query{
		{Origin: daedalus.Cell{X: 1, Y: 1}, Radius: 3},
		{Origin: daedalus.Cell{X: 4, Y: 1}, Radius: 2},
	}
	want, err := vision.Answer(context.Background(), grid, queries)
	require.NoError(t, err)
	wireQueries := make([]*daedalusv1.VisibilityQuery, len(queries))
	for index, query := range queries {
		wireQueries[index] = &daedalusv1.VisibilityQuery{Origin: &daedalusv1.Cell{X: query.Origin.X, Y: query.Origin.Y}, Radius: query.Radius}
	}
	server := New(nil, NewAdmission(1))
	got, err := server.ComputeVisibility(context.Background(), &daedalusv1.ComputeVisibilityRequest{
		OpacityGrid: &daedalusv1.OpacityGrid{Width: grid.Width, Height: grid.Height, Transparent: append([]byte(nil), grid.Transparent...)},
		Queries:     wireQueries,
	})
	require.NoError(t, err)
	require.Len(t, got.GetFields(), len(want))
	for index := range want {
		assert.True(t, bytes.Equal(want[index].Visible, got.Fields[index].GetVisible()))
	}
}

func TestComputeVisibilityHonorsDeadlineWhileAdmissionIsHeld(t *testing.T) {
	t.Parallel()
	server := New(nil, NewAdmission(1))
	server.admission.tokens <- struct{}{}
	server.admission.mutex.Lock()
	server.admission.active.Add(1)
	server.admission.mutex.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := server.ComputeVisibility(ctx, visibilityRequest())
	assert.Equal(t, codes.DeadlineExceeded, status.Code(err))
	server.admission.release()
}

func TestComputeVisibilityShutdownWhileWaitingReturnsUnavailableOrCanceled(t *testing.T) {
	t.Parallel()
	server := New(nil, NewAdmission(1))
	server.admission.tokens <- struct{}{}
	server.admission.mutex.Lock()
	server.admission.active.Add(1)
	server.admission.mutex.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := server.ComputeVisibility(context.Background(), visibilityRequest())
		done <- err
	}()
	server.BeginShutdown()
	err := <-done
	require.Error(t, err)
	assert.Contains(t, []codes.Code{codes.Unavailable, codes.Canceled}, status.Code(err))
	server.admission.release()
}

func visibilityRequest() *daedalusv1.ComputeVisibilityRequest {
	return &daedalusv1.ComputeVisibilityRequest{
		OpacityGrid: &daedalusv1.OpacityGrid{Width: 2, Height: 1, Transparent: []byte{3}},
		Queries:     []*daedalusv1.VisibilityQuery{{Origin: &daedalusv1.Cell{X: 0, Y: 0}, Radius: 1}},
	}
}
