package app

import (
	"errors"
	"reflect"
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/evidence"
	"github.com/example/aac-block-masonry-admission-closure/internal/mass"
	"github.com/example/aac-block-masonry-admission-closure/internal/retest"
)

func TestModel_RetestCreationAdvancesVersionChain(t *testing.T) {
	type fixture struct {
		svc             *Service
		taskID          string
		body            string
		versionAtRetest domain.Version
		retestSet       retest.RetestSet
	}

	setup := func(t *testing.T) fixture {
		t.Helper()
		statePath := t.TempDir() + "/state.json"
		svc := newTestService(t, statePath)
		created := createTask(t, svc, "retest-version-chain")
		body := "body-retest-version-chain"
		mixer := acquire(t, svc, created.TaskID, mass.ResourceMixer, "pan-1", 1)
		commands := []Command{
			{OperationID: "dose", ExpectedVersion: 0, LogicalTime: 2, LeaseToken: mixer.Token, Kind: "dosing", Body: body, MixPan: "pan-1", Materials: map[string]int64{"cement": 900, "water": 450, "aluminum_suspension": 45}},
			{OperationID: "mix", ExpectedVersion: 1, LogicalTime: 3, LeaseToken: mixer.Token, Kind: "mixing", Body: body, MixPan: "pan-1"},
			{OperationID: "pour", ExpectedVersion: 2, LogicalTime: 4, LeaseToken: mixer.Token, Kind: "pouring", Body: body, MixPan: "pan-1"},
			{OperationID: "rise", ExpectedVersion: 3, LogicalTime: 5, Kind: "rising", Body: body, PreHeightUM: 500, PostHeightUM: 750},
		}
		for _, cmd := range commands {
			if _, err := svc.SubmitCommand(created.TaskID, cmd); err != nil {
				t.Fatalf("submit %s: %v", cmd.Kind, err)
			}
		}
		standing := acquire(t, svc, created.TaskID, mass.ResourceStandingBay, body, 6)
		if _, err := svc.SubmitCommand(created.TaskID, Command{OperationID: "stand", ExpectedVersion: 4, LogicalTime: 6, LeaseToken: standing.Token, Kind: "standing", Body: body}); err != nil {
			t.Fatalf("submit standing: %v", err)
		}
		mold := acquire(t, svc, created.TaskID, mass.ResourceMold, body, 7)
		if _, err := svc.SubmitCommand(created.TaskID, Command{OperationID: "demold", ExpectedVersion: 5, LogicalTime: 7, LeaseToken: mold.Token, Kind: "demold", Body: body}); err != nil {
			t.Fatalf("submit demold: %v", err)
		}
		cut := acquire(t, svc, created.TaskID, mass.ResourceCutLine, body, 8)
		grid := &evidence.Grid{Body: evidence.BodyBounds{WidthUM: 100, HeightUM: 100, DepthUM: 100}, Cells: []evidence.CutCell{{Width: 100, Height: 100, Depth: 100}}}
		if _, err := svc.SubmitCommand(created.TaskID, Command{OperationID: "long-cut", ExpectedVersion: 6, LogicalTime: 8, LeaseToken: cut.Token, Kind: "longitudinal_cut", Body: body, Grid: grid}); err != nil {
			t.Fatalf("submit longitudinal cut: %v", err)
		}
		if _, err := svc.SubmitCommand(created.TaskID, Command{
			OperationID: "cross-cut", ExpectedVersion: 7, LogicalTime: 9, LeaseToken: cut.Token,
			Kind: "cross_cut", Body: body, Grid: grid, WireWindow: "wire-window-1",
			Blocks: []BlockAllocation{{ID: "b1", MassGrams: 1000}, {ID: "b2", MassGrams: 350}}, OffcutGrams: 20, WasteGrams: 25,
		}); err != nil {
			t.Fatalf("submit cross cut: %v", err)
		}
		before, err := svc.GetTask(created.TaskID)
		if err != nil {
			t.Fatalf("get task before retest: %v", err)
		}
		rs, err := svc.CreateRetest(RetestRequest{TaskID: created.TaskID, OperationID: "broken-wire-retest", Anomaly: retest.AnomalyBrokenWire, Source: "b1", LogicalTime: 10})
		if err != nil {
			t.Fatalf("create retest: %v", err)
		}
		svc = newTestService(t, statePath)
		return fixture{svc: svc, taskID: created.TaskID, body: body, versionAtRetest: before.Version, retestSet: rs}
	}

	tests := []struct {
		name  string
		check func(*testing.T, fixture)
	}{
		{
			name: "public task and final slot join the new version",
			check: func(t *testing.T, f fixture) {
				got, err := f.svc.GetTask(f.taskID)
				if err != nil {
					t.Fatalf("get task after retest: %v", err)
				}
				wantVersion := f.versionAtRetest + 1
				if got.Generation != f.retestSet.Generation || got.Version != wantVersion {
					t.Fatalf("task after retest has generation/version %d/%d, want %d/%d", got.Generation, got.Version, f.retestSet.Generation, wantVersion)
				}
				verdict, err := f.svc.SubmitVerdict(VerdictRequest{TaskID: f.taskID, Kind: retest.VerdictCancel, ExpectedVersion: wantVersion, Reason: "cancel retest", Credential: "cancel-after-retest", LogicalTime: 11})
				if err != nil {
					t.Fatalf("current public version was rejected by final slot: %v", err)
				}
				if verdict.Verdict == nil || verdict.Verdict.Version != wantVersion {
					t.Fatalf("verdict version = %+v, want %d", verdict.Verdict, wantVersion)
				}
			},
		},
		{
			name: "pre-retest command version is stale",
			check: func(t *testing.T, f fixture) {
				lease := acquire(t, f.svc, f.taskID, mass.ResourceKilnCarPos, f.body, 11)
				_, err := f.svc.SubmitCommand(f.taskID, Command{OperationID: "stale-group", ExpectedVersion: f.versionAtRetest, LogicalTime: 11, LeaseToken: lease.Token, Kind: "grouping", Body: f.body, Positions: map[string]int64{"b1": 1, "b2": 2}})
				var domainErr *domain.Error
				if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeGenerationConflict {
					t.Fatalf("stale command error = %v, want %s", err, domain.CodeGenerationConflict)
				}
			},
		},
		{
			name: "same anomaly member set is idempotent",
			check: func(t *testing.T, f fixture) {
				again, err := f.svc.CreateRetest(RetestRequest{TaskID: f.taskID, OperationID: "broken-wire-retest-again", Anomaly: retest.AnomalyBrokenWire, Source: "b1", LogicalTime: 11})
				if err != nil {
					t.Fatalf("get existing retest: %v", err)
				}
				if !reflect.DeepEqual(again, f.retestSet) {
					t.Fatalf("idempotent retest = %+v, want %+v", again, f.retestSet)
				}
				got, err := f.svc.GetTask(f.taskID)
				if err != nil {
					t.Fatalf("get task after idempotent retest: %v", err)
				}
				if got.Generation != f.retestSet.Generation || got.Version != f.versionAtRetest+1 {
					t.Fatalf("idempotent retest advanced task again to generation/version %d/%d", got.Generation, got.Version)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, setup(t))
		})
	}
}
