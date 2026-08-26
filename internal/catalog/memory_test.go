package catalog

import "testing"

func testSnapshot() RecipeRuleSnapshot {
	return RecipeRuleSnapshot{
		Version:       "v1",
		RecipeSummary: "aac-basic",
		Materials: []MaterialRange{
			{Class: MaterialCement, MinG: 100, MaxG: 200},
		},
		AllowedBatches: []BatchRef{
			{Class: MaterialCement, Batch: "cem-1"},
		},
		ReclaimMaxPPM: 30000,
	}
}

func TestMemoryDirectoryLockSuccess(t *testing.T) {
	d := NewMemoryDirectory()
	s := testSnapshot()
	d.Register(s)
	req := LockRequest{
		RuleVersion: "v1",
		Batches:     []BatchRef{{Class: MaterialCement, Batch: "cem-1"}},
		RecipeHash:  s.SummaryHash(),
		BodyIDs:     []string{"body-1"},
	}
	got, reasons := d.ValidateLock(req)
	if len(reasons) != 0 {
		t.Fatalf("unexpected reasons: %+v", reasons)
	}
	if got.Version != "v1" {
		t.Fatalf("got version %s", got.Version)
	}
}

func TestMemoryDirectoryStaleRule(t *testing.T) {
	d := NewMemoryDirectory()
	d.Register(testSnapshot())
	_, reasons := d.ValidateLock(LockRequest{RuleVersion: "missing"})
	if len(reasons) == 0 || reasons[0].Code != "STALE_RULE" {
		t.Fatalf("expected stale rule reason, got %+v", reasons)
	}
}

func TestMemoryDirectoryBatchMismatch(t *testing.T) {
	d := NewMemoryDirectory()
	s := testSnapshot()
	d.Register(s)
	req := LockRequest{
		RuleVersion: "v1",
		Batches:     []BatchRef{{Class: MaterialCement, Batch: "cem-other"}},
		RecipeHash:  s.SummaryHash(),
		BodyIDs:     []string{"body-1"},
	}
	_, reasons := d.ValidateLock(req)
	if len(reasons) == 0 || reasons[0].Code != "RAW_BATCH_MISMATCH" {
		t.Fatalf("expected batch mismatch reason, got %+v", reasons)
	}
}
