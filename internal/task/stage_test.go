package task

import (
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

func TestStageStrictPrefix(t *testing.T) {
	s := NewStageTracker()
	body := "b1"
	steps := []Stage{StageDosing, StageMixing, StagePouring, StageRising, StageStanding, StageDemold, StageLongCut, StageCrossCut, StageGrouping, StageAutoclave, StageCooling}
	for i, st := range steps {
		if err := s.Advance(body, st, int64(i+1), domain.LogicalTime(i+1), domain.OperationID("op-"+st)); err != nil {
			t.Fatalf("advance %s: %v", st, err)
		}
	}
	if !s.Completed(body) {
		t.Fatal("body should be completed")
	}
}

func TestStageSkipRejected(t *testing.T) {
	s := NewStageTracker()
	if err := s.Advance("b1", StageDosing, 1, 1, "op1"); err != nil {
		t.Fatal(err)
	}
	// Skip mixing and standing; try standing directly.
	if err := s.Advance("b1", StageStanding, 2, 2, "op2"); err == nil {
		t.Fatal("expected skip rejection")
	}
}

func TestStageRepeatDifferentOpConflict(t *testing.T) {
	s := NewStageTracker()
	if err := s.Advance("b1", StageDosing, 1, 1, "op1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance("b1", StageDosing, 1, 1, "op2"); err == nil {
		t.Fatal("expected idempotency conflict on mismatched repeat")
	}
}

func TestStageRepeatSameOpIdempotent(t *testing.T) {
	s := NewStageTracker()
	if err := s.Advance("b1", StageDosing, 1, 1, "op1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance("b1", StageDosing, 1, 1, "op1"); err != nil {
		t.Fatalf("expected idempotent repeat: %v", err)
	}
	if got := len(s.Stages("b1")); got != 1 {
		t.Fatalf("expected 1 stage, got %d", got)
	}
}

func TestStageLogicalTimeReversed(t *testing.T) {
	s := NewStageTracker()
	if err := s.Advance("b1", StageDosing, 1, 10, "op1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance("b1", StageMixing, 2, 5, "op2"); err == nil {
		t.Fatal("expected logical-time-reversed error")
	}
}
