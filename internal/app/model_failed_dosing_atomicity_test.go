package app_test

import (
	"reflect"
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/app"
	"github.com/example/aac-block-masonry-admission-closure/internal/catalog"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/mass"
	"github.com/example/aac-block-masonry-admission-closure/internal/store"
)

func TestModel_FailedDosingLeavesTaskRetryable(t *testing.T) {
	tests := []struct {
		name      string
		materials map[string]int64
	}{
		{name: "negative cement", materials: map[string]int64{"cement": -1, "water": 250, "aluminum_suspension": 25}},
		{name: "negative water", materials: map[string]int64{"cement": 500, "water": -1, "aluminum_suspension": 25}},
		{name: "negative aluminum suspension", materials: map[string]int64{"cement": 500, "water": 250, "aluminum_suspension": -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := catalog.RecipeRuleSnapshot{
				Version:       "v1",
				RecipeSummary: "aac-basic",
				ReclaimMaxPPM: 300_000,
				AllowedBatches: []catalog.BatchRef{
					{Class: catalog.MaterialCement, Batch: "cem-1"},
				},
				Materials: []catalog.MaterialRange{
					{Class: catalog.MaterialCement, MinG: 1, MaxG: 1_000_000},
					{Class: catalog.MaterialWater, MinG: 1, MaxG: 1_000_000},
					{Class: catalog.MaterialAluminum, MinG: 1, MaxG: 1_000_000},
				},
			}
			directory := catalog.NewMemoryDirectory()
			directory.Register(rules)
			svc, err := app.NewService(directory, store.New(""))
			if err != nil {
				t.Fatal(err)
			}
			created, err := svc.CreateTask(app.CreateTaskRequest{
				OperationID:     "create-" + domain.OperationID(tt.name),
				Factory:         "factory",
				ProductionBatch: tt.name,
				RuleVersion:     rules.Version,
				Batches:         []catalog.BatchRef{{Class: catalog.MaterialCement, Batch: "cem-1"}},
				RecipeHash:      rules.SummaryHash(),
				BodyIDs:         []string{"body-1"},
				RawGrams:        map[string]int64{"cement": 1000, "water": 500, "aluminum_suspension": 50},
			})
			if err != nil {
				t.Fatal(err)
			}
			lease, err := svc.AcquireLease(app.LeaseRequest{
				TaskID:       created.TaskID,
				ResourceType: mass.ResourceMixer,
				ResourceID:   "pan-1",
				Holder:       "operator",
				LogicalTime:  1,
				Duration:     100,
			})
			if err != nil {
				t.Fatal(err)
			}

			beforeTask, err := svc.GetTask(created.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			beforeBalance, err := svc.GetMassBalance(created.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			beforeLineage, err := svc.GetLineage(created.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			beforeEvidence, err := svc.GetEvidence(created.TaskID)
			if err != nil {
				t.Fatal(err)
			}

			_, err = svc.SubmitCommand(created.TaskID, app.Command{
				OperationID:     "invalid-dose",
				ExpectedVersion: created.Version,
				LogicalTime:     2,
				LeaseToken:      lease.Token,
				Kind:            "dosing",
				Body:            "body-1",
				MixPan:          "pan-1",
				Materials:       tt.materials,
			})
			if err == nil {
				t.Fatal("negative dosing command succeeded")
			}
			if domainErr, ok := err.(*domain.Error); !ok || domainErr.Code != domain.CodeInvalidArgument {
				t.Fatalf("negative dosing error = %v, want INVALID_ARGUMENT", err)
			}

			afterTask, _ := svc.GetTask(created.TaskID)
			afterBalance, _ := svc.GetMassBalance(created.TaskID)
			afterLineage, _ := svc.GetLineage(created.TaskID)
			afterEvidence, _ := svc.GetEvidence(created.TaskID)
			if !reflect.DeepEqual(afterTask, beforeTask) {
				t.Fatalf("failed command changed task: before=%+v after=%+v", beforeTask, afterTask)
			}
			if !reflect.DeepEqual(afterBalance, beforeBalance) {
				t.Fatalf("failed command changed ledger: before=%v after=%v", beforeBalance, afterBalance)
			}
			if !reflect.DeepEqual(afterLineage, beforeLineage) {
				t.Fatalf("failed command changed lineage: before=%+v after=%+v", beforeLineage, afterLineage)
			}
			if !reflect.DeepEqual(afterEvidence, beforeEvidence) {
				t.Fatalf("failed command changed evidence: before=%+v after=%+v", beforeEvidence, afterEvidence)
			}

			valid := app.Command{
				OperationID:     "valid-dose",
				ExpectedVersion: created.Version,
				LogicalTime:     2,
				LeaseToken:      lease.Token,
				Kind:            "dosing",
				Body:            "body-1",
				MixPan:          "pan-1",
				Materials:       map[string]int64{"cement": 500, "water": 250, "aluminum_suspension": 25},
			}
			first, err := svc.SubmitCommand(created.TaskID, valid)
			if err != nil {
				t.Fatalf("valid retry after failed command: %v", err)
			}
			replay, err := svc.SubmitCommand(created.TaskID, valid)
			if err != nil {
				t.Fatalf("idempotent replay after valid retry: %v", err)
			}
			if !reflect.DeepEqual(replay, first) {
				t.Fatalf("idempotent replay changed result: first=%+v replay=%+v", first, replay)
			}
			if _, err := svc.SubmitCommand(created.TaskID, app.Command{
				OperationID: "mix", ExpectedVersion: first.Version, LogicalTime: 3,
				LeaseToken: lease.Token, Kind: "mixing", Body: "body-1", MixPan: "pan-1",
			}); err != nil {
				t.Fatalf("strict next stage after recovered dosing: %v", err)
			}
		})
	}
}
