package task

import "testing"

func TestLineageAddNodeDuplicate(t *testing.T) {
	g := NewGraph()
	if err := g.AddNode(MaterialNode{ID: "n1"}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddNode(MaterialNode{ID: "n1"}); err == nil {
		t.Fatal("expected duplicate node error")
	}
}

func TestLineageMultipleParent(t *testing.T) {
	g := NewGraph()
	for _, id := range []string{"a", "b"} {
		if err := g.AddNode(MaterialNode{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	// A block node enforces a single owner.
	if err := g.AddNode(MaterialNode{ID: "c", Kind: NodeBlock}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEdge(LineageEdge{Parent: "a", Child: "c", Type: EdgeCut}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEdge(LineageEdge{Parent: "b", Child: "c", Type: EdgeCut}); err == nil {
		t.Fatal("expected multiple-parent error")
	}
}

func TestLineageMixPanMultipleParentsOK(t *testing.T) {
	g := NewGraph()
	for _, id := range []string{"cement", "water", "pan"} {
		if err := g.AddNode(MaterialNode{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	// A mix pan may legitimately aggregate several raw batches.
	if err := g.AddEdge(LineageEdge{Parent: "cement", Child: "pan", Type: EdgeDose}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEdge(LineageEdge{Parent: "water", Child: "pan", Type: EdgeDose}); err != nil {
		t.Fatalf("mix pan should accept multiple raw parents: %v", err)
	}
}

func TestLineageCycle(t *testing.T) {
	g := NewGraph()
	for _, id := range []string{"a", "b"} {
		if err := g.AddNode(MaterialNode{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.AddEdge(LineageEdge{Parent: "a", Child: "b", Type: EdgeMix}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEdge(LineageEdge{Parent: "b", Child: "a", Type: EdgeMix}); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestLineageSingleParentOK(t *testing.T) {
	g := NewGraph()
	for _, id := range []string{"a", "b"} {
		if err := g.AddNode(MaterialNode{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.AddEdge(LineageEdge{Parent: "a", Child: "b", Type: EdgePour}); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}
