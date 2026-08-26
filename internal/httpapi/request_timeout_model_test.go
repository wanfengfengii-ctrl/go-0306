package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/aac-block-masonry-admission-closure/internal/app"
	"github.com/example/aac-block-masonry-admission-closure/internal/catalog"
	"github.com/example/aac-block-masonry-admission-closure/internal/store"
)

type timeoutModelPersister struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
	start    sync.Once
	finish   sync.Once

	mu    sync.Mutex
	saves []store.Snapshot
}

func newTimeoutModelPersister() *timeoutModelPersister {
	return &timeoutModelPersister{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
}

func (p *timeoutModelPersister) Load() (store.Snapshot, error) {
	return store.Snapshot{Schema: store.Schema}, nil
}

func (p *timeoutModelPersister) Save(snap store.Snapshot) error {
	p.start.Do(func() { close(p.started) })
	<-p.release
	p.record(snap)
	p.finish.Do(func() { close(p.finished) })
	return nil
}

// SaveContext lets a context-aware persistence boundary stop before making a
// durable change while retaining compatibility with the public Persister seam.
func (p *timeoutModelPersister) SaveContext(ctx context.Context, snap store.Snapshot) error {
	p.start.Do(func() { close(p.started) })
	select {
	case <-ctx.Done():
		p.finish.Do(func() { close(p.finished) })
		return ctx.Err()
	case <-p.release:
		p.record(snap)
		p.finish.Do(func() { close(p.finished) })
		return nil
	}
}

func (p *timeoutModelPersister) record(snap store.Snapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.saves = append(p.saves, snap)
}

func (p *timeoutModelPersister) snapshots() []store.Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]store.Snapshot(nil), p.saves...)
}

func timeoutModelRecipe() catalog.RecipeRuleSnapshot {
	return catalog.RecipeRuleSnapshot{
		Version:       "timeout-v1",
		RecipeSummary: "timeout-model",
		Materials: []catalog.MaterialRange{
			{Class: catalog.MaterialCement, MinG: 1, MaxG: 10_000},
		},
		AllowedBatches: []catalog.BatchRef{
			{Class: catalog.MaterialCement, Batch: "cement-timeout"},
		},
	}
}

func timeoutModelRequest(snapshot catalog.RecipeRuleSnapshot) app.CreateTaskRequest {
	return app.CreateTaskRequest{
		OperationID:     "op-timeout-model",
		Factory:         "factory-timeout",
		ProductionBatch: "batch-timeout",
		RuleVersion:     snapshot.Version,
		Batches:         append([]catalog.BatchRef(nil), snapshot.AllowedBatches...),
		RecipeHash:      snapshot.SummaryHash(),
		BodyIDs:         []string{"body-timeout"},
		RawGrams:        map[string]int64{"cement": 100},
	}
}

func timeoutModelHTTPCall(handler http.Handler, req app.CreateTaskRequest) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		var body bytes.Buffer
		_ = json.NewEncoder(&body).Encode(req)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/tasks", &body))
		done <- recorder
	}()
	return done
}

func TestModel_RequestTimeoutCommitBoundary(t *testing.T) {
	tests := []struct {
		name string
		mode string
	}{
		{name: "timed out create is JSON and leaves no late commit", mode: "timeout"},
		{name: "disabled timeout still permits a slow successful commit", mode: "disabled"},
		{name: "domain errors keep their normal JSON mapping", mode: "domain-error"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := timeoutModelRecipe()
			directory := catalog.NewMemoryDirectory()
			directory.Register(snapshot)

			switch tc.mode {
			case "timeout":
				persister := newTimeoutModelPersister()
				released := false
				t.Cleanup(func() {
					if !released {
						close(persister.release)
					}
				})
				service, err := app.NewService(directory, persister)
				if err != nil {
					t.Fatal(err)
				}
				server := NewServer(service)
				server.RequestTimeout = 100 * time.Millisecond
				done := timeoutModelHTTPCall(server.Handler(), timeoutModelRequest(snapshot))

				select {
				case <-persister.started:
				case <-time.After(time.Second):
					t.Fatal("create never reached the persistence boundary")
				}

				var recorder *httptest.ResponseRecorder
				select {
				case recorder = <-done:
				case <-time.After(time.Second):
					t.Fatal("request did not return at its configured timeout")
				}
				if recorder.Code != http.StatusServiceUnavailable {
					t.Fatalf("timeout status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
				}
				if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
					t.Fatalf("timeout Content-Type = %q, want application/json", contentType)
				}
				var body ErrorBody
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatalf("timeout body is not the stable JSON error envelope: %v; body=%q", err, recorder.Body.String())
				}
				if body.Code == "" || body.Message != "request timed out" || !body.Retryable {
					t.Fatalf("timeout error = %+v, want a coded retryable request timed out error", body)
				}

				close(persister.release)
				released = true
				select {
				case <-persister.finished:
				case <-time.After(time.Second):
					t.Fatal("persistence call did not finish after release")
				}

				query := httptest.NewRecorder()
				server.Handler().ServeHTTP(query, httptest.NewRequest(http.MethodGet, "/v1/tasks/factory-timeout-batch-timeout", nil))
				if query.Code != http.StatusBadRequest {
					t.Fatalf("timed-out request changed task state: GET status = %d, body=%s", query.Code, query.Body.String())
				}
				for i, saved := range persister.snapshots() {
					if len(saved.Tasks) != 0 || len(saved.Idempotency) != 0 || len(saved.EventLog) != 0 {
						t.Fatalf("timed-out request changed durable snapshot %d: tasks=%d idempotency=%d events=%d", i, len(saved.Tasks), len(saved.Idempotency), len(saved.EventLog))
					}
				}

			case "disabled":
				persister := newTimeoutModelPersister()
				released := false
				t.Cleanup(func() {
					if !released {
						close(persister.release)
					}
				})
				service, err := app.NewService(directory, persister)
				if err != nil {
					t.Fatal(err)
				}
				server := NewServer(service)
				server.RequestTimeout = 0
				done := timeoutModelHTTPCall(server.Handler(), timeoutModelRequest(snapshot))
				select {
				case <-persister.started:
				case <-time.After(time.Second):
					t.Fatal("create never reached the persistence boundary")
				}
				select {
				case recorder := <-done:
					t.Fatalf("request returned before persistence completed: status=%d body=%s", recorder.Code, recorder.Body.String())
				default:
				}
				close(persister.release)
				released = true
				select {
				case recorder := <-done:
					if recorder.Code != http.StatusCreated {
						t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
					}
					if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
						t.Fatalf("success Content-Type = %q, want application/json", contentType)
					}
				case <-time.After(time.Second):
					t.Fatal("request did not complete after persistence was released")
				}

			case "domain-error":
				service, err := app.NewService(directory, store.New(""))
				if err != nil {
					t.Fatal(err)
				}
				server := NewServer(service)
				request := timeoutModelRequest(snapshot)
				request.RuleVersion = "missing"
				recorder := <-timeoutModelHTTPCall(server.Handler(), request)
				if recorder.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
				}
				var body ErrorBody
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatalf("domain error is not JSON: %v", err)
				}
				if body.Code != "INVALID_ARGUMENT" {
					t.Fatalf("domain error code = %q, want INVALID_ARGUMENT", body.Code)
				}
			}
		})
	}
}
