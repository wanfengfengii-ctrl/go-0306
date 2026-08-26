package httpapi

import (
	"net/http"
	"strconv"

	"github.com/example/aac-block-masonry-admission-closure/internal/app"
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req app.CreateTaskRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	res, err := s.svc.CreateTask(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	var cmd app.Command
	if err := decode(r, &cmd); err != nil {
		writeError(w, err)
		return
	}
	res, err := s.svc.SubmitCommand(taskID(r), cmd)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleAcquireLease(w http.ResponseWriter, r *http.Request) {
	var req app.LeaseRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.TaskID = taskID(r)
	res, err := s.svc.AcquireLease(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) handleRenewLease(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Now       domain.LogicalTime `json:"now"`
		NewExpiry domain.LogicalTime `json:"new_expiry"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	res, err := s.svc.RenewLease(taskID(r), domain.Token(r.PathValue("token")), req.Now, req.NewExpiry)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleRegisterDeviceCall(w http.ResponseWriter, r *http.Request) {
	var req app.DeviceCallRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.TaskID = taskID(r)
	res, err := s.svc.RegisterDeviceCall(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) handleDeviceAttempt(w http.ResponseWriter, r *http.Request) {
	var req app.DeviceAttemptRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.TaskID = taskID(r)
	req.CallID = r.PathValue("callID")
	res, err := s.svc.RecordDeviceAttempt(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleCreateRetest(w http.ResponseWriter, r *http.Request) {
	var req app.RetestRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.TaskID = taskID(r)
	res, err := s.svc.CreateRetest(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) handleGetRetest(w http.ResponseWriter, r *http.Request) {
	gen, err := strconv.ParseInt(r.PathValue("generation"), 10, 64)
	if err != nil {
		writeError(w, errBadGeneration)
		return
	}
	res, err := s.svc.GetRetest(taskID(r), domain.Generation(gen))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSubmitReview(w http.ResponseWriter, r *http.Request) {
	var req app.ReviewRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.TaskID = taskID(r)
	res, err := s.svc.SubmitReview(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) handleSubmitVerdict(w http.ResponseWriter, r *http.Request) {
	var req app.VerdictRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.TaskID = taskID(r)
	res, err := s.svc.SubmitVerdict(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.GetTask(taskID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetLineage(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.GetLineage(taskID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetMassBalance(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.GetMassBalance(taskID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetEvidence(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.GetEvidence(taskID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetVerdict(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.GetVerdict(taskID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"verdict": res})
}

var errBadGeneration = &domain.Error{Code: domain.CodeInvalidArgument, Message: "invalid generation"}
