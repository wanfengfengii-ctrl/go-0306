package task

import (
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// BodyStage is one recorded, append-only stage event for a single body. It
// carries the body identity, canonical stage, monotonic sequence, logical time
// and originating operation so the strict prefix invariant can be rebuilt and
// re-verified after restart.
type BodyStage struct {
	Body        string             `json:"body"`
	Stage       Stage              `json:"stage"`
	Sequence    int64              `json:"sequence"`
	LogicalTime domain.LogicalTime `json:"logical_time"`
	OperationID domain.OperationID `json:"operation_id"`
	Valid       bool               `json:"valid"`
}

// StageTracker enforces the strict body-stage prefix "dosing → mixing →
// pouring → rising → standing → demold → longitudinal_cut → cross_cut →
// grouping → autoclave → cooling". It rejects skips, wrong-generation advance,
// reversed logical time and non-idempotent repeats.
type StageTracker struct {
	bodies map[string][]BodyStage
}

// NewStageTracker returns an empty stage tracker.
func NewStageTracker() *StageTracker {
	return &StageTracker{bodies: make(map[string][]BodyStage)}
}

// Stages returns the recorded stage events for a body in append order.
func (s *StageTracker) Stages(body string) []BodyStage { return s.bodies[body] }

// Load replaces the tracker with a recovered snapshot.
func (s *StageTracker) Load(events []BodyStage) {
	s.bodies = make(map[string][]BodyStage)
	for _, e := range events {
		s.bodies[e.Body] = append(s.bodies[e.Body], e)
	}
}

// CurrentStage returns the last valid stage reached by a body.
func (s *StageTracker) CurrentStage(body string) (Stage, bool) {
	hist := s.bodies[body]
	if len(hist) == 0 {
		return "", false
	}
	return hist[len(hist)-1].Stage, true
}

// Completed reports whether the body has reached the terminal cooling stage.
func (s *StageTracker) Completed(body string) bool {
	st, ok := s.CurrentStage(body)
	return ok && st == StageCooling
}

// Advance records one stage event, enforcing the strict prefix and idempotent
// repeat semantics. A repeat of the already-current stage succeeds only when
// the operation id matches, in which case it is an idempotent no-op returning
// the existing event. Any skip, reversed sequence/time or mismatched repeat
// returns a stable error and does not mutate state.
func (s *StageTracker) Advance(body string, st Stage, seq int64, t domain.LogicalTime, op domain.OperationID) error {
	idx := StageIndex(st)
	if idx < 0 {
		return domain.Newf(domain.CodeStageOutOfOrder, "unknown stage %q", st)
	}
	hist := s.bodies[body]
	if len(hist) == 0 {
		if idx != 0 {
			return domain.Newf(domain.CodeStageOutOfOrder, "first stage for %s must be dosing, got %s", body, st)
		}
		s.bodies[body] = []BodyStage{{Body: body, Stage: st, Sequence: seq, LogicalTime: t, OperationID: op, Valid: true}}
		return nil
	}
	last := hist[len(hist)-1]
	lastIdx := StageIndex(last.Stage)
	switch {
	case idx == lastIdx:
		if last.OperationID != op {
			return domain.Newf(domain.CodeIdempotencyConflict, "stage %s already recorded by %s", st, last.OperationID)
		}
		return nil
	case idx != lastIdx+1:
		return domain.Newf(domain.CodeStageOutOfOrder, "stage %s cannot follow %s", st, last.Stage)
	case seq <= last.Sequence:
		return domain.Newf(domain.CodeLogicalTimeReversed, "sequence %d not after %d", seq, last.Sequence)
	case !t.After(last.LogicalTime):
		return domain.Newf(domain.CodeLogicalTimeReversed, "logical time %d not after %d", t, last.LogicalTime)
	}
	s.bodies[body] = append(hist, BodyStage{Body: body, Stage: st, Sequence: seq, LogicalTime: t, OperationID: op, Valid: true})
	return nil
}
