package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/example/aac-block-masonry-admission-closure/internal/app"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// Server is the HTTP front end. It owns parameter decoding, timeout application
// and domain error mapping; it never mutates domain state directly, delegating
// every decision to the application service.
type Server struct {
	svc            *app.Service
	RequestTimeout time.Duration
}

// NewServer builds a Server around the application service.
func NewServer(svc *app.Service) *Server {
	return &Server{svc: svc, RequestTimeout: 10 * time.Second}
}

// Handler returns the configured HTTP mux with the full command/query surface.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/tasks", s.handleCreateTask)
	mux.HandleFunc("POST /v1/tasks/{taskID}/commands", s.handleCommand)
	mux.HandleFunc("POST /v1/tasks/{taskID}/leases", s.handleAcquireLease)
	mux.HandleFunc("POST /v1/tasks/{taskID}/leases/{token}/renew", s.handleRenewLease)
	mux.HandleFunc("POST /v1/tasks/{taskID}/device-calls", s.handleRegisterDeviceCall)
	mux.HandleFunc("POST /v1/tasks/{taskID}/device-calls/{callID}/attempts", s.handleDeviceAttempt)
	mux.HandleFunc("POST /v1/tasks/{taskID}/retests", s.handleCreateRetest)
	mux.HandleFunc("GET /v1/tasks/{taskID}/retests/{generation}", s.handleGetRetest)
	mux.HandleFunc("POST /v1/tasks/{taskID}/reviews", s.handleSubmitReview)
	mux.HandleFunc("POST /v1/tasks/{taskID}/verdicts", s.handleSubmitVerdict)
	mux.HandleFunc("GET /v1/tasks/{taskID}", s.handleGetTask)
	mux.HandleFunc("GET /v1/tasks/{taskID}/lineage", s.handleGetLineage)
	mux.HandleFunc("GET /v1/tasks/{taskID}/mass-balance", s.handleGetMassBalance)
	mux.HandleFunc("GET /v1/tasks/{taskID}/evidence", s.handleGetEvidence)
	mux.HandleFunc("GET /v1/tasks/{taskID}/verdict", s.handleGetVerdict)
	return s.withTimeout(mux)
}

// withTimeout wraps the mux with a per-request timeout context.
func (s *Server) withTimeout(next http.Handler) http.Handler {
	if s.RequestTimeout <= 0 {
		return next
	}
	return http.TimeoutHandler(next, s.RequestTimeout, "request timed out")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Health(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &domain.Error{Code: domain.CodeInvalidArgument, Message: "invalid request body: " + err.Error()}
	}
	return nil
}

func taskID(r *http.Request) string { return r.PathValue("taskID") }
