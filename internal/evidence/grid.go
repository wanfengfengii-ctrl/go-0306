package evidence

import (
	"math"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// BodyBounds is an integer-micron cuboid describing a locked valid body.
type BodyBounds struct {
	WidthUM  int64 `json:"width_um"`
	HeightUM int64 `json:"height_um"`
	DepthUM  int64 `json:"depth_um"`
}

// Grid is a three-dimensional cut grid over a body, produced from integer
// micron coordinates. A valid grid non-degenerately and non-overlappingly
// covers the whole body with non-negative allowances.
type Grid struct {
	Body  BodyBounds `json:"body"`
	Cells []CutCell  `json:"cells"`
}

// Validate checks the grid invariants: all cells within bounds, non-degenerate
// positive dimensions, non-negative notch and margin, complete non-overlapping
// coverage, and overflow-safe volume arithmetic. Failures return stable error
// codes.
func (g Grid) Validate() error {
	if g.Body.WidthUM <= 0 || g.Body.HeightUM <= 0 || g.Body.DepthUM <= 0 {
		return domain.New(domain.CodeGridInvalid, "body dimensions must be positive")
	}
	bodyVol, err := volume(g.Body.WidthUM, g.Body.HeightUM, g.Body.DepthUM)
	if err != nil {
		return err
	}
	var covered int64
	for i := range g.Cells {
		c := &g.Cells[i]
		if err := validateCell(*c, g.Body); err != nil {
			return err
		}
		v, err := volume(c.Width, c.Height, c.Depth)
		if err != nil {
			return err
		}
		if c.Notch < 0 {
			return domain.New(domain.CodeGridInvalid, "notch must be non-negative")
		}
		if c.Margin < 0 {
			return domain.New(domain.CodeGridInvalid, "margin must be non-negative")
		}
		// subtract notch volume from the covered region conservatively
		if v < c.Notch {
			return domain.New(domain.CodeGridInvalid, "notch exceeds cell volume")
		}
		v -= c.Notch
		sum, ok := addInt64(covered, v)
		if !ok {
			return domain.New(domain.CodeFixedPointOverflow, "coverage volume overflow")
		}
		covered = sum
	}
	for i := range g.Cells {
		for j := i + 1; j < len(g.Cells); j++ {
			if overlap(g.Cells[i], g.Cells[j]) {
				return domain.New(domain.CodeGridInvalid, "cells overlap")
			}
		}
	}
	if covered != bodyVol {
		return domain.Newf(domain.CodeGridInvalid, "coverage incomplete: covered %d of %d", covered, bodyVol)
	}
	return nil
}

func validateCell(c CutCell, b BodyBounds) error {
	if c.Width <= 0 || c.Height <= 0 || c.Depth <= 0 {
		return domain.New(domain.CodeGridInvalid, "cell dimensions must be positive")
	}
	if c.X < 0 || c.Y < 0 || c.Z < 0 {
		return domain.New(domain.CodeGridInvalid, "cell origin must be non-negative")
	}
	if c.X+c.Width > b.WidthUM || c.Y+c.Height > b.HeightUM || c.Z+c.Depth > b.DepthUM {
		return domain.New(domain.CodeGridInvalid, "cell exceeds body bounds")
	}
	return nil
}

func overlap(a, b CutCell) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width &&
		a.Y < b.Y+b.Height && b.Y < a.Y+a.Height &&
		a.Z < b.Z+b.Depth && b.Z < a.Z+a.Depth
}

func volume(w, h, d int64) (int64, error) {
	if w <= 0 || h <= 0 || d <= 0 {
		return 0, domain.New(domain.CodeGridInvalid, "volume dimensions must be positive")
	}
	if w > math.MaxInt64/h || w > math.MaxInt64/d {
		return 0, domain.New(domain.CodeFixedPointOverflow, "volume multiplication overflow")
	}
	wh := w * h
	if wh > math.MaxInt64/d {
		return 0, domain.New(domain.CodeFixedPointOverflow, "volume multiplication overflow")
	}
	return wh * d, nil
}

func addInt64(a, b int64) (int64, bool) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, false
	}
	return s, true
}
