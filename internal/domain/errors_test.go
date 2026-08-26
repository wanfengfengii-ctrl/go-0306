package domain

import "testing"

func TestErrorWithReasonsSorted(t *testing.T) {
	e := New(CodeGridInvalid, "bad grid").WithReasons(
		Reason{Code: CodeGridInvalid, Field: "b", Msg: "second"},
		Reason{Code: CodeGridInvalid, Field: "a", Msg: "first"},
	)
	if len(e.Reasons) != 2 {
		t.Fatalf("got %d reasons, want 2", len(e.Reasons))
	}
	if e.Reasons[0].Field != "a" || e.Reasons[1].Field != "b" {
		t.Fatalf("reasons not sorted: %+v", e.Reasons)
	}
}

func TestErrorFormatting(t *testing.T) {
	e := New(CodeStaleRule, "stale summary").WithOperation("op-1")
	if e.Error() == "" {
		t.Fatal("empty error string")
	}
	if e.Code != CodeStaleRule {
		t.Fatalf("got code %s", e.Code)
	}
}

func TestErrorRetryable(t *testing.T) {
	e := New(CodeLeaseConflict, "conflict").WithRetryable(true)
	if !e.Retryable {
		t.Fatal("expected retryable")
	}
}
