package app

import (
	"reflect"
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/retest"
	"github.com/example/aac-block-masonry-admission-closure/internal/store"
	"github.com/example/aac-block-masonry-admission-closure/internal/task"
)

func TestModel_TerminalTaskRetestBarrier(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *Service, string, *store.Store)
	}{
		{
			name: "open task creates one generation and reuses its unique key",
			run: func(t *testing.T, svc *Service, taskID string, st *store.Store) {
				req := RetestRequest{TaskID: taskID, OperationID: "retest-open", Anomaly: retest.AnomalyCollapse, Source: "body-open", LogicalTime: 1}
				first, err := svc.CreateRetest(req)
				if err != nil {
					t.Fatalf("create retest on open task: %v", err)
				}
				if first.Generation != 1 {
					t.Fatalf("generation = %d, want 1", first.Generation)
				}
				beforeReplay, err := st.Load()
				if err != nil {
					t.Fatal(err)
				}

				replayed, err := svc.CreateRetest(req)
				if err != nil {
					t.Fatalf("replay retest unique key: %v", err)
				}
				if !reflect.DeepEqual(replayed, first) {
					t.Fatalf("unique-key replay = %+v, want %+v", replayed, first)
				}
				afterReplay, err := st.Load()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(afterReplay, beforeReplay) {
					t.Fatal("unique-key replay changed the persistent snapshot")
				}
			},
		},
		{
			name: "final verdict rejects a new retest without changing terminal state",
			run: func(t *testing.T, svc *Service, taskID string, st *store.Store) {
				final, err := svc.SubmitVerdict(VerdictRequest{TaskID: taskID, Kind: retest.VerdictCancel, ExpectedVersion: 0, Reason: "terminal", Credential: "credential-terminal", LogicalTime: 1})
				if err != nil {
					t.Fatalf("submit terminal verdict: %v", err)
				}
				if final.Verdict == nil {
					t.Fatal("terminal verdict was not stored")
				}
				before, err := st.Load()
				if err != nil {
					t.Fatal(err)
				}

				if _, err := svc.CreateRetest(RetestRequest{TaskID: taskID, OperationID: "late-retest", Anomaly: retest.AnomalySurfaceCrack, Source: "late-source", LogicalTime: 2}); err == nil {
					t.Fatal("CreateRetest succeeded after the final verdict")
				}
				taskAfter, err := svc.GetTask(taskID)
				if err != nil {
					t.Fatal(err)
				}
				if taskAfter.Status != task.TaskClosed || taskAfter.Generation != 0 || taskAfter.Version != 1 {
					t.Fatalf("terminal task changed after rejected retest: %+v", taskAfter)
				}
				if _, err := svc.GetRetest(taskID, 1); err == nil {
					t.Fatal("rejected retest created generation 1")
				}
				gotVerdict, err := svc.GetVerdict(taskID)
				if err != nil || !reflect.DeepEqual(gotVerdict, final.Verdict) {
					t.Fatalf("terminal verdict query = %+v, %v; want %+v", gotVerdict, err, final.Verdict)
				}
				repeated, err := svc.SubmitVerdict(VerdictRequest{TaskID: taskID, Kind: retest.VerdictAdmit, ExpectedVersion: 99, Credential: "replacement"})
				if err != nil || !repeated.Already || !reflect.DeepEqual(repeated.Verdict, final.Verdict) {
					t.Fatalf("repeat verdict = %+v, %v; want existing terminal verdict", repeated, err)
				}
				after, err := st.Load()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(after, before) {
					t.Fatal("late retest or terminal reads changed the persistent snapshot")
				}
			},
		},
		{
			name: "closed status alone rejects a new retest without changing snapshot",
			run: func(t *testing.T, svc *Service, taskID string, st *store.Store) {
				snap, err := st.Load()
				if err != nil {
					t.Fatal(err)
				}
				ts := snap.Tasks[taskID]
				ts.Task.Status = task.TaskClosed
				snap.Tasks[taskID] = ts
				if err := st.Save(snap); err != nil {
					t.Fatal(err)
				}
				svc = newTestService(t, st.Path())
				before, err := st.Load()
				if err != nil {
					t.Fatal(err)
				}

				if _, err := svc.CreateRetest(RetestRequest{TaskID: taskID, OperationID: domain.OperationID("closed-retest"), Anomaly: retest.AnomalyBrokenWire, Source: "closed-source", LogicalTime: 2}); err == nil {
					t.Fatal("CreateRetest succeeded for a task whose status is closed")
				}
				after, err := st.Load()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(after, before) {
					t.Fatal("retest on closed task changed generation, quality facts, resources, version, or snapshot")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := store.New(t.TempDir() + "/state.json")
			svc := newTestService(t, st.Path())
			created := createTask(t, svc, "terminal-retest")
			tc.run(t, svc, created.TaskID, st)
		})
	}
}
