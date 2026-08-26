package app

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/catalog"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/retest"
	"github.com/example/aac-block-masonry-admission-closure/internal/store"
	"github.com/example/aac-block-masonry-admission-closure/internal/task"
)

type verdictMemoryStore struct {
	snapshot store.Snapshot
}

func (s *verdictMemoryStore) Load() (store.Snapshot, error) { return s.snapshot, nil }
func (*verdictMemoryStore) Save(store.Snapshot) error       { return nil }

func TestModel_ConcurrentVerdictsPublishOneCredential(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(32)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousProcs) })

	tests := []struct {
		name  string
		kinds []retest.VerdictKind
	}{
		{name: "admit competes with isolate", kinds: []retest.VerdictKind{retest.VerdictAdmit, retest.VerdictIsolate}},
		{name: "admit competes with cancel", kinds: []retest.VerdictKind{retest.VerdictAdmit, retest.VerdictCancel}},
		{name: "isolate competes with cancel", kinds: []retest.VerdictKind{retest.VerdictIsolate, retest.VerdictCancel}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for attempt := 0; attempt < 200; attempt++ {
				taskID := fmt.Sprintf("terminal-%d", attempt)
				version := domain.Version(12)
				bodyID := "body-" + taskID
				seed := store.Snapshot{
					Schema: store.Schema,
					Tasks: map[string]store.TaskSnapshot{
						taskID: {
							Task: task.ProductionTask{
								ID: taskID, Generation: 0, Status: task.TaskInProgress,
								Version: version, BodyIDs: []string{bodyID},
							},
							Stages: []task.BodyStage{{Body: bodyID, Stage: task.StageCooling, Sequence: 11, LogicalTime: 11, Valid: true}},
							Reviews: []retest.Review{
								{Person: "reviewer-a", Qualified: true, Generation: 0},
								{Person: "reviewer-b", Qualified: true, Generation: 0},
							},
							Final: retest.FinalSlot{Task: taskID, Version: version},
						},
					},
				}
				svc, err := NewService(catalog.NewMemoryDirectory(), &verdictMemoryStore{snapshot: seed})
				if err != nil {
					t.Fatalf("create service: %v", err)
				}

				const competitors = 64
				start := make(chan struct{})
				responses := make(chan VerdictResponse, competitors)
				errors := make(chan error, competitors)
				var ready sync.WaitGroup
				ready.Add(competitors)
				for i := 0; i < competitors; i++ {
					go func(i int) {
						ready.Done()
						<-start
						res, err := svc.SubmitVerdict(VerdictRequest{
							TaskID: taskID, Kind: tt.kinds[i%len(tt.kinds)], ExpectedVersion: version,
							Credential: fmt.Sprintf("credential-%d", i), Reason: "terminal arbitration",
						})
						responses <- res
						errors <- err
					}(i)
				}
				ready.Wait()
				close(start)

				published := map[string]bool{}
				for i := 0; i < competitors; i++ {
					res := <-responses
					<-errors
					if res.Verdict != nil {
						published[res.Verdict.Credential] = true
					}
				}
				final, err := svc.GetVerdict(taskID)
				if err != nil {
					t.Fatalf("query final verdict: %v", err)
				}
				if final == nil {
					t.Fatal("concurrent terminal requests published no final verdict")
				}
				published[final.Credential] = true
				if len(published) != 1 {
					t.Fatalf("concurrent terminal requests published different credentials %v; stored final is %q", published, final.Credential)
				}
			}
		})
	}
}
