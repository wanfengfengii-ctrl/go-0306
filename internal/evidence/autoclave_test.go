package evidence

import (
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

func fx(s int64) domain.Fixed { f, _ := domain.NewFixed(s, 0); return f }

func TestAnalyzeAutoclaveValid(t *testing.T) {
	hold := fx(1200)
	pts := []AutoclavePoint{
		{LogicalTime: 0, Pressure: fx(0)},
		{LogicalTime: 10, Pressure: fx(1200)},
		{LogicalTime: 30, Pressure: fx(1200)},
		{LogicalTime: 40, Pressure: fx(0)},
	}
	res, err := AnalyzeAutoclave(pts, hold)
	if err != nil {
		t.Fatal(err)
	}
	if res.HoldStart != 10 || res.HoldEnd != 30 {
		t.Fatalf("hold window %d..%d", res.HoldStart, res.HoldEnd)
	}
	if res.EquivalentHold.Scaled() != 20 {
		t.Fatalf("equivalent hold %d, want 20", res.EquivalentHold.Scaled())
	}
	if res.RampUpRate.Scaled() != 120 {
		t.Fatalf("ramp up %d, want 120", res.RampUpRate.Scaled())
	}
}

func TestAnalyzeAutoclaveReversedTime(t *testing.T) {
	pts := []AutoclavePoint{
		{LogicalTime: 10, Pressure: fx(0)},
		{LogicalTime: 5, Pressure: fx(1200)},
		{LogicalTime: 15, Pressure: fx(0)},
	}
	if _, err := AnalyzeAutoclave(pts, fx(1200)); err == nil {
		t.Fatal("expected reversed time error")
	}
}

func TestAnalyzeAutoclaveGap(t *testing.T) {
	pts := []AutoclavePoint{
		{LogicalTime: 0, Pressure: fx(0)},
		{LogicalTime: 10, Pressure: fx(1200)},
		{LogicalTime: 20, Pressure: fx(500)}, // gap below hold
		{LogicalTime: 30, Pressure: fx(1200)},
		{LogicalTime: 40, Pressure: fx(0)},
	}
	if _, err := AnalyzeAutoclave(pts, fx(1200)); err == nil {
		t.Fatal("expected hold gap error")
	}
}

func TestAnalyzeAutoclaveNeverReachesHold(t *testing.T) {
	pts := []AutoclavePoint{
		{LogicalTime: 0, Pressure: fx(0)},
		{LogicalTime: 10, Pressure: fx(100)},
		{LogicalTime: 20, Pressure: fx(0)},
	}
	if _, err := AnalyzeAutoclave(pts, fx(1200)); err == nil {
		t.Fatal("expected never-reaches-hold error")
	}
}
