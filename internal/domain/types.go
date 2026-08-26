package domain

// LogicalTime is a monotonic logical clock value. It is the only notion of
// time used for ordering and lease expiry, guaranteeing deterministic,
// cross-platform behavior independent of wall-clock values.
type LogicalTime int64

// OperationID uniquely identifies an idempotent command submission within a
// scope.
type OperationID string

// Version is the optimistic concurrency version attached to an aggregate.
type Version int64

// Generation identifies a task generation. Generation 0 is the initial lock;
// each retest supersession increments it.
type Generation int64

// Token is an opaque lease token used for renewal and release matching.
type Token string

// Compare orders two generations.
func (g Generation) Compare(o Generation) int {
	switch {
	case g < o:
		return -1
	case g > o:
		return 1
	default:
		return 0
	}
}

// Before reports whether t strictly precedes o.
func (t LogicalTime) Before(o LogicalTime) bool { return t < o }

// After reports whether t strictly follows o.
func (t LogicalTime) After(o LogicalTime) bool { return t > o }
