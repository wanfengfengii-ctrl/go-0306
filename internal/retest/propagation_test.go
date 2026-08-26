package retest

import (
	"reflect"
	"testing"
)

func TestPropagateAdjacencyAndWire(t *testing.T) {
	topo := BlockTopology{
		Pan:        map[string]string{"b1": "p1", "b2": "p1", "b3": "p2"},
		Position:   map[string]int64{"b1": 1, "b2": 2, "b3": 3},
		WireWindow: map[string]string{"b1": "w1", "b2": "w2", "b3": "w1"},
		RawBatch:   map[string]string{"b1": "r1", "b2": "r1", "b3": "r1"},
	}
	got := Propagate("b1", topo, []string{"b1", "b2", "b3"})
	// b1 (self), b2 (adjacent position 2), b3 (shared wire w1)
	want := []string{"b1", "b2", "b3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRetestKeyDeterministic(t *testing.T) {
	a := RetestKey(AnomalyCollapse, "src", []string{"b1", "b2"})
	b := RetestKey(AnomalyCollapse, "src", []string{"b2", "b1"})
	if a == b {
		t.Fatal("key should differ for different member order")
	}
	c := RetestKey(AnomalyCollapse, "src", []string{"b1", "b2"})
	if a != c {
		t.Fatal("key should be stable for identical input")
	}
}

func TestReviewBoardAdmitEligible(t *testing.T) {
	b := NewReviewBoard()
	if err := b.Submit(Review{Person: "a", Qualified: true, Generation: 0}); err != nil {
		t.Fatal(err)
	}
	if err := b.AdmitEligible(0); err == nil {
		t.Fatal("expected ineligible with one reviewer")
	}
	if err := b.Submit(Review{Person: "a", Qualified: true, Generation: 0}); err != nil {
		t.Fatal(err)
	}
	if err := b.AdmitEligible(0); err == nil {
		t.Fatal("same person twice must not qualify")
	}
	if err := b.Submit(Review{Person: "b", Qualified: true, Generation: 0}); err != nil {
		t.Fatal(err)
	}
	if err := b.AdmitEligible(0); err != nil {
		t.Fatalf("expected eligible with two distinct reviewers: %v", err)
	}
}
