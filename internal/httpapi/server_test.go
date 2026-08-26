package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/app"
	"github.com/example/aac-block-masonry-admission-closure/internal/catalog"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/store"
)

func testSnapshot() catalog.RecipeRuleSnapshot {
	return catalog.RecipeRuleSnapshot{
		Version:       "v1",
		RecipeSummary: "aac-basic",
		ReclaimMaxPPM: 300000,
		Materials: []catalog.MaterialRange{
			{Class: catalog.MaterialCement, MinG: 1, MaxG: 1_000_000},
		},
		AllowedBatches: []catalog.BatchRef{
			{Class: catalog.MaterialCement, Batch: "cem-1"},
		},
	}
}

func newTestServer() *Server {
	dir := catalog.NewMemoryDirectory()
	dir.Register(testSnapshot())
	svc, _ := app.NewService(dir, store.New(""))
	return NewServer(svc)
}

func doJSON(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHealthOK(t *testing.T) {
	s := newTestServer()
	rec := doJSON(t, s, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
}

func TestHealthMethodNotAllowed(t *testing.T) {
	s := newTestServer()
	rec := doJSON(t, s, http.MethodPost, "/healthz", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got status %d, want 405", rec.Code)
	}
}

func TestCreateTaskHTTP(t *testing.T) {
	s := newTestServer()
	snap := testSnapshot()
	rec := doJSON(t, s, http.MethodPost, "/v1/tasks", app.CreateTaskRequest{
		OperationID:     "op-create",
		Factory:         "f1",
		ProductionBatch: "p1",
		RuleVersion:     "v1",
		Batches:         []catalog.BatchRef{{Class: catalog.MaterialCement, Batch: "cem-1"}},
		RecipeHash:      snap.SummaryHash(),
		BodyIDs:         []string{"body-1"},
		RawGrams:        map[string]int64{"cement": 1000},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var res app.CreateTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.TaskID != "f1-p1" {
		t.Fatalf("task id %s", res.TaskID)
	}
}

func TestCreateTaskStaleRuleHTTP(t *testing.T) {
	s := newTestServer()
	rec := doJSON(t, s, http.MethodPost, "/v1/tasks", app.CreateTaskRequest{
		RuleVersion: "missing",
		BodyIDs:     []string{"body-1"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
	var eb ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil {
		t.Fatal(err)
	}
	if eb.Code != string(domain.CodeInvalidArgument) {
		t.Fatalf("unexpected code %s", eb.Code)
	}
}

func TestUnknownTaskQuery(t *testing.T) {
	s := newTestServer()
	rec := doJSON(t, s, http.MethodGet, "/v1/tasks/nope", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}
