package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/app"
	"github.com/example/aac-block-masonry-admission-closure/internal/catalog"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/evidence"
	"github.com/example/aac-block-masonry-admission-closure/internal/httpapi"
	"github.com/example/aac-block-masonry-admission-closure/internal/store"
)

func TestModel_DeviceCallIDsJoinRegistrationToDurableRetryChains(t *testing.T) {
	type registrationStyle int
	const (
		registrationInBody registrationStyle = iota
		registrationInPath
	)

	type registrationResponse struct {
		ID     string `json:"id"`
		CallID string `json:"call_id"`
	}

	snapshot := catalog.RecipeRuleSnapshot{
		Version:       "v1",
		RecipeSummary: "aac-cold-region-school",
		ReclaimMaxPPM: 300000,
		Materials: []catalog.MaterialRange{
			{Class: catalog.MaterialCement, MinG: 1, MaxG: 1_000_000},
		},
		AllowedBatches: []catalog.BatchRef{
			{Class: catalog.MaterialCement, Batch: "cement-cold-1"},
		},
	}

	doJSON := func(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var encoded bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&encoded).Encode(body); err != nil {
				t.Fatal(err)
			}
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(method, path, &encoded))
		return recorder
	}

	newService := func(t *testing.T, statePath string) *app.Service {
		t.Helper()
		directory := catalog.NewMemoryDirectory()
		directory.Register(snapshot)
		svc, err := app.NewService(directory, store.New(statePath))
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}

	createTask := func(t *testing.T, svc *app.Service, batch string) string {
		t.Helper()
		created, err := svc.CreateTask(app.CreateTaskRequest{
			OperationID:     domain.OperationID("create-" + batch),
			Factory:         "school-factory",
			ProductionBatch: batch,
			RuleVersion:     snapshot.Version,
			Batches:         snapshot.AllowedBatches,
			RecipeHash:      snapshot.SummaryHash(),
			BodyIDs:         []string{"aac-body-1"},
			RawGrams:        map[string]int64{"cement": 1000},
		})
		if err != nil {
			t.Fatal(err)
		}
		return created.TaskID
	}

	register := func(t *testing.T, handler http.Handler, taskID, callID, device string, style *registrationStyle) evidence.DeviceCall {
		t.Helper()
		body := map[string]any{
			"call_id":        callID,
			"device":         device,
			"request_digest": "sha256:" + callID,
			"logical_time":   1,
		}
		path := "/v1/tasks/" + taskID + "/device-calls"
		response := doJSON(t, handler, http.MethodPost, path, body)
		chosen := registrationInBody
		if response.Code != http.StatusCreated {
			delete(body, "call_id")
			path += "/" + callID
			response = doJSON(t, handler, http.MethodPost, path, body)
			chosen = registrationInPath
		}
		if response.Code != http.StatusCreated {
			t.Fatalf("register %q: got status %d, want 201: %s", callID, response.Code, response.Body.String())
		}
		var call evidence.DeviceCall
		if err := json.Unmarshal(response.Body.Bytes(), &call); err != nil {
			t.Fatal(err)
		}
		var identity registrationResponse
		if err := json.Unmarshal(response.Body.Bytes(), &identity); err != nil {
			t.Fatal(err)
		}
		returnedID := identity.ID
		if returnedID == "" {
			returnedID = identity.CallID
		}
		if returnedID == "" || returnedID != callID {
			t.Fatalf("registration identity = %q, want non-empty %q; body: %s", returnedID, callID, response.Body.String())
		}
		*style = chosen
		return call
	}

	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "different call IDs persist independent timeout malformed and success sequences",
			run: func(t *testing.T) {
				statePath := t.TempDir() + "/state.json"
				svc := newService(t, statePath)
				taskID := createTask(t, svc, "batch-retry")
				handler := httpapi.NewServer(svc).Handler()

				var style registrationStyle
				register(t, handler, taskID, "call-press-1", "pressure-machine", &style)
				var secondStyle registrationStyle
				register(t, handler, taskID, "call-gauge-2", "temperature-gauge", &secondStyle)
				if style != secondStyle {
					t.Fatalf("registration style changed between distinct calls: %d then %d", style, secondStyle)
				}

				attemptPath := "/v1/tasks/" + taskID + "/device-calls/call-press-1/attempts"
				for _, attempt := range []struct {
					kind        string
					logicalTime int64
				}{
					{kind: "timeout", logicalTime: 2},
					{kind: "malformed", logicalTime: 3},
				} {
					response := doJSON(t, handler, http.MethodPost, attemptPath, map[string]any{
						"kind": attempt.kind, "logical_time": attempt.logicalTime,
					})
					if response.Code != http.StatusBadRequest {
						t.Fatalf("%s attempt: got status %d, want 400: %s", attempt.kind, response.Code, response.Body.String())
					}
					var failure httpapi.ErrorBody
					if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
						t.Fatal(err)
					}
					if failure.Code != string(domain.CodeDeviceRetryPending) || !failure.Retryable {
						t.Fatalf("%s attempt error = %+v, want retryable %s", attempt.kind, failure, domain.CodeDeviceRetryPending)
					}
				}

				// Recreate the service to ensure the pending calls and their retry counters
				// are joined by call ID through the durable public boundary.
				svc = newService(t, statePath)
				handler = httpapi.NewServer(svc).Handler()
				response := doJSON(t, handler, http.MethodPost, attemptPath, map[string]any{
					"kind": "success", "reading": map[string]any{"scaled": 4250, "scale": 2}, "logical_time": 4,
				})
				if response.Code != http.StatusOK {
					t.Fatalf("press success: got status %d, want 200: %s", response.Code, response.Body.String())
				}
				var press evidence.DeviceCall
				if err := json.Unmarshal(response.Body.Bytes(), &press); err != nil {
					t.Fatal(err)
				}
				if press.ID != "call-press-1" || press.RetrySeq != 3 || press.Status != evidence.CallSucceeded || !press.HasReading || press.Reading.Scaled() != 4250 {
					t.Fatalf("press retry chain = %+v, want succeeded call-press-1 at retry 3 with its reading", press)
				}

				response = doJSON(t, handler, http.MethodPost, "/v1/tasks/"+taskID+"/device-calls/call-gauge-2/attempts", map[string]any{
					"kind": "success", "reading": map[string]any{"scaled": -180, "scale": 1}, "logical_time": 5,
				})
				if response.Code != http.StatusOK {
					t.Fatalf("gauge success: got status %d, want 200: %s", response.Code, response.Body.String())
				}
				var gauge evidence.DeviceCall
				if err := json.Unmarshal(response.Body.Bytes(), &gauge); err != nil {
					t.Fatal(err)
				}
				if gauge.ID != "call-gauge-2" || gauge.RetrySeq != 1 || gauge.Status != evidence.CallSucceeded || !gauge.HasReading || gauge.Reading.Scaled() != -180 {
					t.Fatalf("gauge retry chain = %+v, want independent succeeded call-gauge-2 at retry 1", gauge)
				}
			},
		},
		{
			name: "registering the same call ID remains a duplicate conflict",
			run: func(t *testing.T) {
				svc := newService(t, "")
				taskID := createTask(t, svc, "batch-duplicate")
				handler := httpapi.NewServer(svc).Handler()
				var style registrationStyle
				register(t, handler, taskID, "call-press-1", "pressure-machine", &style)

				body := map[string]any{
					"device": "pressure-machine", "request_digest": "sha256:call-press-1", "logical_time": 2,
				}
				path := "/v1/tasks/" + taskID + "/device-calls/call-press-1"
				if style == registrationInBody {
					body["call_id"] = "call-press-1"
					path = "/v1/tasks/" + taskID + "/device-calls"
				}
				response := doJSON(t, handler, http.MethodPost, path, body)
				if response.Code != http.StatusBadRequest {
					t.Fatalf("duplicate registration: got status %d, want 400: %s", response.Code, response.Body.String())
				}
				var failure httpapi.ErrorBody
				if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
					t.Fatal(err)
				}
				if failure.Code != string(domain.CodeDuplicateBody) || failure.Retryable {
					t.Fatalf("duplicate error = %+v, want non-retryable %s", failure, domain.CodeDuplicateBody)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
