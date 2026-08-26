package evidence

import "testing"

func TestGridValidFullCoverage(t *testing.T) {
	g := Grid{
		Body: BodyBounds{WidthUM: 100, HeightUM: 100, DepthUM: 100},
		Cells: []CutCell{
			{Width: 100, Height: 100, Depth: 100},
		},
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("expected valid grid, got %v", err)
	}
}

func TestGridCellExceedsBounds(t *testing.T) {
	g := Grid{
		Body: BodyBounds{WidthUM: 10, HeightUM: 10, DepthUM: 10},
		Cells: []CutCell{
			{Width: 20, Height: 10, Depth: 10},
		},
	}
	if err := g.Validate(); err == nil {
		t.Fatal("expected out-of-bounds error")
	}
}

func TestGridDegenerateCell(t *testing.T) {
	g := Grid{
		Body: BodyBounds{WidthUM: 10, HeightUM: 10, DepthUM: 10},
		Cells: []CutCell{
			{Width: 0, Height: 10, Depth: 10},
		},
	}
	if err := g.Validate(); err == nil {
		t.Fatal("expected degenerate cell error")
	}
}

func TestGridIncompleteCoverage(t *testing.T) {
	g := Grid{
		Body: BodyBounds{WidthUM: 100, HeightUM: 100, DepthUM: 100},
		Cells: []CutCell{
			{Width: 50, Height: 100, Depth: 100},
		},
	}
	if err := g.Validate(); err == nil {
		t.Fatal("expected incomplete coverage error")
	}
}

func TestGridOverlap(t *testing.T) {
	g := Grid{
		Body: BodyBounds{WidthUM: 100, HeightUM: 100, DepthUM: 100},
		Cells: []CutCell{
			{Width: 100, Height: 100, Depth: 100},
			{Width: 10, Height: 10, Depth: 10},
		},
	}
	if err := g.Validate(); err == nil {
		t.Fatal("expected overlap error")
	}
}

func TestGridNegativeNotch(t *testing.T) {
	g := Grid{
		Body: BodyBounds{WidthUM: 100, HeightUM: 100, DepthUM: 100},
		Cells: []CutCell{
			{Width: 100, Height: 100, Depth: 100, Notch: -1},
		},
	}
	if err := g.Validate(); err == nil {
		t.Fatal("expected negative notch error")
	}
}
