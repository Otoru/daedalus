package vision

import (
	"context"
	"fmt"

	"github.com/Otoru/daedalus"
)

// Query identifies one observer and its inclusive Euclidean vision radius.
type Query struct {
	Origin daedalus.Cell
	Radius uint32
}

// Answer computes one independent field for every query, preserving query
// order. The returned fields have detached visibility buffers.
func Answer(ctx context.Context, grid OpacityGrid, queries []Query) ([]Field, error) {
	return AnswerInto(ctx, nil, grid, queries)
}

// AnswerInto computes a batch into reusable destination fields. Request
// validation and the initial cancellation check happen before dst is grown or
// any existing field is changed.
func AnswerInto(ctx context.Context, dst []Field, grid OpacityGrid, queries []Query) ([]Field, error) {
	if err := validateQueries(grid, queries); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(queries) == 0 {
		if dst == nil {
			return []Field{}, nil
		}
		return dst[:0], nil
	}
	if cap(dst) < len(queries) {
		dst = make([]Field, len(queries))
	} else {
		dst = dst[:len(queries)]
	}
	for index, query := range queries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := ComputeInto(ctx, &dst[index], grid, query.Origin, query.Radius); err != nil {
			return nil, err
		}
	}
	return dst, nil
}

func validateQueries(grid OpacityGrid, queries []Query) error {
	if err := grid.Validate(); err != nil {
		return err
	}
	if len(queries) > MaxQueries {
		return fmt.Errorf("%w: query count %d exceeds %d", daedalus.ErrLimitExceeded, len(queries), MaxQueries)
	}
	for index, query := range queries {
		if query.Radius > MaxRadius {
			return fmt.Errorf("%w: query %d radius %d exceeds %d", daedalus.ErrLimitExceeded, index, query.Radius, MaxRadius)
		}
		if !grid.TransparentAt(query.Origin) {
			return fmt.Errorf("%w: query %d origin (%d,%d) is outside or opaque", daedalus.ErrInvalidVisibility, index, query.Origin.X, query.Origin.Y)
		}
	}
	return nil
}
