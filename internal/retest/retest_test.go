package retest

import "testing"

func TestFinalSlotSingleWrite(t *testing.T) {
	s := &FinalSlot{Task: "t1", Version: 0}
	v := FinalVerdict{Task: "t1", Kind: VerdictAdmit, Credential: "cred-1"}
	if err := s.Set(v, 0); err != nil {
		t.Fatal(err)
	}
	if s.Verdict == nil || s.Verdict.Credential != "cred-1" {
		t.Fatalf("unexpected verdict: %+v", s.Verdict)
	}
}

func TestFinalSlotAlreadySet(t *testing.T) {
	s := &FinalSlot{Task: "t1", Version: 0}
	if err := s.Set(FinalVerdict{Task: "t1", Kind: VerdictAdmit, Credential: "c1"}, 0); err != nil {
		t.Fatal(err)
	}
	err := s.Set(FinalVerdict{Task: "t1", Kind: VerdictIsolate, Credential: "c2"}, 1)
	if err == nil {
		t.Fatal("expected final-already-set error")
	}
	if s.Verdict.Kind != VerdictAdmit {
		t.Fatalf("verdict changed to %s", s.Verdict.Kind)
	}
}

func TestFinalSlotStaleVersion(t *testing.T) {
	s := &FinalSlot{Task: "t1", Version: 5}
	err := s.Set(FinalVerdict{Task: "t1", Kind: VerdictAdmit, Credential: "c1"}, 4)
	if err == nil {
		t.Fatal("expected stale version error")
	}
	if s.Verdict != nil {
		t.Fatal("verdict should remain nil")
	}
}
