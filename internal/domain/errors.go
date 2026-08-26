package domain

import (
	"fmt"
	"sort"
	"strings"
)

// Code is a stable machine-readable error code shared across every public
// interface. The set is fixed by the component traceability and must not be
// silently widened by callers.
type Code string

const (
	CodeStaleRule            Code = "STALE_RULE"
	CodeRawBatchMismatch     Code = "RAW_BATCH_MISMATCH"
	CodeDuplicateBody        Code = "DUPLICATE_BODY"
	CodeLineageCycle         Code = "LINEAGE_CYCLE"
	CodeMultipleParent       Code = "MULTIPLE_PARENT"
	CodeMaterialOverdraw     Code = "MATERIAL_OVERDRAW"
	CodeReclaimRatioExceeded Code = "RECLAIM_RATIO_EXCEEDED"
	CodeStageOutOfOrder      Code = "STAGE_OUT_OF_ORDER"
	CodeGridInvalid          Code = "GRID_INVALID"
	CodeWireLifeExceeded     Code = "WIRE_LIFE_EXCEEDED"
	CodeLeaseConflict        Code = "LEASE_CONFLICT"
	CodeLeaseExpired         Code = "LEASE_EXPIRED"
	CodeLogicalTimeReversed  Code = "LOGICAL_TIME_REVERSED"
	CodeFixedPointOverflow   Code = "FIXED_POINT_OVERFLOW"
	CodeDeviceRetryPending   Code = "DEVICE_RETRY_PENDING"
	CodeGenerationConflict   Code = "GENERATION_CONFLICT"
	CodeIdempotencyConflict  Code = "IDEMPOTENCY_CONFLICT"
	CodeFinalAlreadySet      Code = "FINAL_ALREADY_SET"
	CodeInvalidArgument      Code = "INVALID_ARGUMENT"
)

// Reason is one ordered, deterministically-sorted validation failure. It
// carries the canonical business keys so batches of errors can be emitted in
// ascending factory, batch, pan, body, cell, position, block and generation
// order as required by the domain rules.
type Reason struct {
	Code  Code   `json:"code"`
	Field string `json:"field,omitempty"`
	Msg   string `json:"message"`
}

// Error is the single stable error value returned across the HTTP boundary.
type Error struct {
	Code        Code     `json:"code"`
	Message     string   `json:"message"`
	OperationID string   `json:"operation_id,omitempty"`
	Retryable   bool     `json:"retryable"`
	Reasons     []Reason `json:"reasons,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	var sb strings.Builder
	sb.WriteString(string(e.Code))
	sb.WriteString(": ")
	sb.WriteString(e.Message)
	if e.OperationID != "" {
		fmt.Fprintf(&sb, " (operation %s)", e.OperationID)
	}
	return sb.String()
}

// New builds an Error without reasons.
func New(code Code, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

// Newf builds an Error with a formatted message.
func Newf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WithOperation attaches the idempotency operation id for stable client
// correlation.
func (e *Error) WithOperation(op string) *Error {
	e.OperationID = op
	return e
}

// WithRetryable marks whether the caller may safely retry the operation.
func (e *Error) WithRetryable(r bool) *Error {
	e.Retryable = r
	return e
}

// WithReasons attaches deterministically-sorted reasons to the error.
func (e *Error) WithReasons(rs ...Reason) *Error {
	e.Reasons = append(e.Reasons, rs...)
	e.sortReasons()
	return e
}

// sortReasons applies the canonical ascending business-key ordering required
// by the domain rules so identical failures always serialize identically.
func (e *Error) sortReasons() {
	sort.SliceStable(e.Reasons, func(i, j int) bool {
		if e.Reasons[i].Field != e.Reasons[j].Field {
			return e.Reasons[i].Field < e.Reasons[j].Field
		}
		if e.Reasons[i].Code != e.Reasons[j].Code {
			return e.Reasons[i].Code < e.Reasons[j].Code
		}
		return e.Reasons[i].Msg < e.Reasons[j].Msg
	})
}
