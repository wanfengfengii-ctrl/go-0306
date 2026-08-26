package app_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/app"
	"github.com/example/aac-block-masonry-admission-closure/internal/catalog"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/retest"
	"github.com/example/aac-block-masonry-admission-closure/internal/store"
)

func TestModel_CreateRetestOperationIdempotency(t *testing.T) {
	tests := []struct {
		name         string
		operationID  domain.OperationID
		anomaly      retest.Anomaly
		source       string
		wantConflict bool
	}{
		{
			name:        "same operation and same payload replays original result",
			operationID: "retest-op-1",
			anomaly:     retest.AnomalyCollapse,
			source:      "block-a",
		},
		{
			name:         "same operation with different anomaly conflicts",
			operationID:  "retest-op-1",
			anomaly:      retest.AnomalyLowStrength,
			source:       "block-a",
			wantConflict: true,
		},
		{
			name:         "same operation with different source conflicts",
			operationID:  "retest-op-1",
			anomaly:      retest.AnomalyCollapse,
			source:       "block-b",
			wantConflict: true,
		},
		{
			name:        "different operation with same canonical retest key returns existing set",
			operationID: "retest-op-2",
			anomaly:     retest.AnomalyCollapse,
			source:      "block-a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := catalog.RecipeRuleSnapshot{
				Version:       "v1",
				RecipeSummary: "idempotent-retests",
			}
			directory := catalog.NewMemoryDirectory()
			directory.Register(rules)
			persistence := store.New(t.TempDir() + "/state.json")
			svc, err := app.NewService(directory, persistence)
			if err != nil {
				t.Fatalf("create service: %v", err)
			}
			created, err := svc.CreateTask(app.CreateTaskRequest{
				OperationID:     "create-task",
				Factory:         "factory",
				ProductionBatch: "batch",
				RuleVersion:     rules.Version,
				RecipeHash:      rules.SummaryHash(),
				BodyIDs:         []string{"body-a"},
			})
			if err != nil {
				t.Fatalf("create task: %v", err)
			}

			firstRequest := app.RetestRequest{
				TaskID:      created.TaskID,
				OperationID: "retest-op-1",
				Anomaly:     retest.AnomalyCollapse,
				Source:      "block-a",
				LogicalTime: 1,
			}
			first, err := svc.CreateRetest(firstRequest)
			if err != nil {
				t.Fatalf("create initial retest: %v", err)
			}
			before, err := svc.GetTask(created.TaskID)
			if err != nil {
				t.Fatalf("get task before retry: %v", err)
			}

			got, retryErr := svc.CreateRetest(app.RetestRequest{
				TaskID:      created.TaskID,
				OperationID: tt.operationID,
				Anomaly:     tt.anomaly,
				Source:      tt.source,
				LogicalTime: 1,
			})
			if tt.wantConflict {
				var domainErr *domain.Error
				if !errors.As(retryErr, &domainErr) || domainErr.Code != domain.CodeIdempotencyConflict {
					t.Fatalf("retry error = %v, want %s", retryErr, domain.CodeIdempotencyConflict)
				}
			} else {
				if retryErr != nil {
					t.Fatalf("idempotent retry: %v", retryErr)
				}
				if !reflect.DeepEqual(got, first) {
					t.Fatalf("retry result = %+v, want original %+v", got, first)
				}
			}

			after, err := svc.GetTask(created.TaskID)
			if err != nil {
				t.Fatalf("get task after retry: %v", err)
			}
			if after.Generation != before.Generation {
				t.Fatalf("task generation advanced from %d to %d", before.Generation, after.Generation)
			}
			if !reflect.DeepEqual(after.Generations, before.Generations) {
				t.Fatalf("current generation changed: before=%+v after=%+v", before.Generations, after.Generations)
			}
			persisted, err := persistence.Load()
			if err != nil {
				t.Fatalf("load persisted task: %v", err)
			}
			if count := len(persisted.Tasks[created.TaskID].Retests); count != 1 {
				t.Fatalf("persisted retest count = %d, want 1", count)
			}
			current, err := svc.GetRetest(created.TaskID, first.Generation)
			if err != nil || !reflect.DeepEqual(current, first) {
				t.Fatalf("current retest = %+v, %v; want original %+v", current, err, first)
			}
		})
	}
}
