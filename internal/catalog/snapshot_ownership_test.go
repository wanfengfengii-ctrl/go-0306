package catalog_test

import (
	"reflect"
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/catalog"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

func TestModel_MemoryDirectorySnapshotOwnership(t *testing.T) {
	newSnapshot := func() catalog.RecipeRuleSnapshot {
		return catalog.RecipeRuleSnapshot{
			Version:       "v1",
			RecipeSummary: "aac-basic",
			Materials: []catalog.MaterialRange{
				{Class: catalog.MaterialCement, MinG: 100, MaxG: 200},
			},
			AllowedBatches: []catalog.BatchRef{
				{Class: catalog.MaterialCement, Batch: "cem-1"},
			},
			ProgramIDs: map[string]string{"autoclave": "program-1"},
			Thresholds: map[string]catalog.ThresholdRule{
				"strength": {Metric: "strength", Scale: 2},
			},
		}
	}
	assertOriginal := func(t *testing.T, d *catalog.MemoryDirectory) {
		t.Helper()
		got, err := d.Snapshot("v1")
		if err != nil {
			t.Fatalf("Snapshot(v1): %v", err)
		}
		want := newSnapshot()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("snapshot changed through caller-owned data:\n got: %#v\nwant: %#v", got, want)
		}

		valid := catalog.LockRequest{
			RuleVersion: "v1",
			Batches:     []catalog.BatchRef{{Class: catalog.MaterialCement, Batch: "cem-1"}},
			RecipeHash:  want.SummaryHash(),
			BodyIDs:     []string{"body-1"},
		}
		locked, reasons := d.ValidateLock(valid)
		if len(reasons) != 0 {
			t.Fatalf("original allowed batch was rejected: %+v", reasons)
		}
		if !reflect.DeepEqual(locked, want) {
			t.Fatalf("lock returned changed snapshot:\n got: %#v\nwant: %#v", locked, want)
		}

		valid.Batches[0].Batch = "cem-2"
		_, reasons = d.ValidateLock(valid)
		if len(reasons) != 1 || reasons[0].Code != domain.CodeRawBatchMismatch {
			t.Fatalf("changed, unauthorized batch should be RAW_BATCH_MISMATCH, got %+v", reasons)
		}
		_, reasons = d.ValidateLock(catalog.LockRequest{RuleVersion: "missing"})
		if len(reasons) != 1 || reasons[0].Code != domain.CodeStaleRule {
			t.Fatalf("unknown rule should be STALE_RULE, got %+v", reasons)
		}
	}

	cases := []struct {
		name   string
		mutate func(*testing.T, *catalog.MemoryDirectory)
	}{
		{
			name: "Register disconnects the caller snapshot",
			mutate: func(t *testing.T, d *catalog.MemoryDirectory) {
				s := newSnapshot()
				d.Register(s)
				s.Materials[0].MaxG = 999
				s.AllowedBatches[0].Batch = "cem-2"
				s.ProgramIDs["autoclave"] = "program-2"
				s.Thresholds["strength"] = catalog.ThresholdRule{Metric: "changed", Scale: 9}
			},
		},
		{
			name: "RegisterAll disconnects restored snapshots",
			mutate: func(t *testing.T, d *catalog.MemoryDirectory) {
				s := newSnapshot()
				restored := map[string]catalog.RecipeRuleSnapshot{"v1": s}
				d.RegisterAll(restored)
				s.Materials[0].MaxG = 999
				s.AllowedBatches[0].Batch = "cem-2"
				s.ProgramIDs["autoclave"] = "program-2"
				s.Thresholds["strength"] = catalog.ThresholdRule{Metric: "changed", Scale: 9}
				delete(restored, "v1")
			},
		},
		{
			name: "Snapshot disconnects returned data",
			mutate: func(t *testing.T, d *catalog.MemoryDirectory) {
				d.Register(newSnapshot())
				s, err := d.Snapshot("v1")
				if err != nil {
					t.Fatalf("Snapshot(v1): %v", err)
				}
				s.Materials[0].MaxG = 999
				s.AllowedBatches[0].Batch = "cem-2"
				s.ProgramIDs["autoclave"] = "program-2"
				s.Thresholds["strength"] = catalog.ThresholdRule{Metric: "changed", Scale: 9}
			},
		},
		{
			name: "ValidateLock disconnects the locked snapshot",
			mutate: func(t *testing.T, d *catalog.MemoryDirectory) {
				want := newSnapshot()
				d.Register(want)
				locked, reasons := d.ValidateLock(catalog.LockRequest{
					RuleVersion: "v1",
					Batches:     []catalog.BatchRef{{Class: catalog.MaterialCement, Batch: "cem-1"}},
					RecipeHash:  want.SummaryHash(),
					BodyIDs:     []string{"body-1"},
				})
				if len(reasons) != 0 {
					t.Fatalf("initial lock failed: %+v", reasons)
				}
				locked.Materials[0].MaxG = 999
				locked.AllowedBatches[0].Batch = "cem-2"
				locked.ProgramIDs["autoclave"] = "program-2"
				locked.Thresholds["strength"] = catalog.ThresholdRule{Metric: "changed", Scale: 9}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := catalog.NewMemoryDirectory()
			tc.mutate(t, d)
			assertOriginal(t, d)
		})
	}
}
