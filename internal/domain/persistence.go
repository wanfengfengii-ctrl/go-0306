package domain

import "encoding/json"

// IdempotencyRecord captures a canonical command submission so identical
// operation ids replay the same result while divergent payloads conflict.
//
// Result is kept as raw JSON rather than an interface value so the record
// round-trips through the durable snapshot without losing the concrete result
// type (and integer precision). Replay sites decode it back into the specific
// response struct.
type IdempotencyRecord struct {
	Scope       string          `json:"scope"`
	OperationID OperationID     `json:"operation_id"`
	RequestHash string          `json:"request_hash"`
	Result      json.RawMessage `json:"result,omitempty"`
	ErrorCode   Code            `json:"error_code,omitempty"`
	CommitVer   Version         `json:"commit_version"`
}

// EventLogEntry is a monotonic append-only event used to rebuild aggregates on
// startup, resume pending device calls and verify snapshot consistency.
type EventLogEntry struct {
	Sequence    int64       `json:"sequence"`
	Task        string      `json:"task"`
	Kind        string      `json:"kind"`
	OperationID OperationID `json:"operation_id"`
	Payload     any         `json:"payload"`
}

// EventLog is a monotonic append-only log.
type EventLog struct {
	seq     int64
	entries []EventLogEntry
}

// NewEventLog returns an empty event log.
func NewEventLog() *EventLog { return &EventLog{} }

// Append adds one entry and returns its monotonic sequence number.
func (l *EventLog) Append(e EventLogEntry) int64 {
	l.seq++
	e.Sequence = l.seq
	l.entries = append(l.entries, e)
	return l.seq
}

// Entries returns the ordered entries.
func (l *EventLog) Entries() []EventLogEntry { return l.entries }
