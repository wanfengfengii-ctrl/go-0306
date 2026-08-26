// Package httpapi implements the Go HTTP API component: JSON command and query
// endpoints, transaction boundaries, stable error responses, deterministic
// ordering, health checks and restart recovery entry points. It decodes
// parameters, validates operation ids, applies timeouts and maps domain errors
// without bypassing the domain aggregates.
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// ErrorBody is the stable error envelope returned by every endpoint.
type ErrorBody struct {
	Code        string          `json:"code"`
	Message     string          `json:"message"`
	OperationID string          `json:"operation_id,omitempty"`
	Retryable   bool            `json:"retryable"`
	Reasons     []domain.Reason `json:"reasons,omitempty"`
}

// writeError serializes a domain error into the stable envelope. Reasons are
// already deterministically sorted by the domain layer.
func writeError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	body := ErrorBody{Code: string(domain.CodeInvalidArgument), Message: err.Error()}
	if de, ok := err.(*domain.Error); ok {
		status = statusFor(de.Code)
		body = ErrorBody{
			Code:        string(de.Code),
			Message:     de.Message,
			OperationID: de.OperationID,
			Retryable:   de.Retryable,
			Reasons:     de.Reasons,
		}
	}
	writeJSON(w, status, body)
}

func statusFor(c domain.Code) int {
	switch c {
	case domain.CodeLeaseConflict, domain.CodeGenerationConflict,
		domain.CodeIdempotencyConflict, domain.CodeFinalAlreadySet:
		return http.StatusConflict
	case domain.CodeStaleRule, domain.CodeLogicalTimeReversed:
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

var (
	errMethodNotAllowed = &domain.Error{Code: domain.CodeInvalidArgument, Message: "method not allowed"}
	domainInvalidBody   = &domain.Error{Code: domain.CodeInvalidArgument, Message: "invalid request body"}
)
