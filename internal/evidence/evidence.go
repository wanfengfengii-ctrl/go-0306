// Package evidence implements the 发气切割及蒸压证据记录器 component: device-call
// status validation, rising/standing sequences, integer-micron three-dimensional
// cut grids, wire usage and kiln-car grouping, plus the appending of temperature
// and pressure points and deterministic integer fixed-point computation of
// expansion ratio, cut yield, pressure ramp rates and equivalent holding time.
package evidence

import (
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// BodyStageEvent is an append-only stage event for one body.
type BodyStageEvent struct {
	Body        string             `json:"body"`
	Stage       string             `json:"stage"`
	Sequence    int64              `json:"sequence"`
	LogicalTime domain.LogicalTime `json:"logical_time"`
	DeviceCall  string             `json:"device_call"`
	LeaseToken  domain.Token       `json:"lease_token"`
	InputDigest string             `json:"input_digest"`
	Valid       bool               `json:"valid"`
}

// Evidence is the union of specialized evidence payloads keyed by kind.
type Evidence struct {
	ID         string            `json:"id"`
	Body       string            `json:"body"`
	Kind       string            `json:"kind"`
	Payload    any               `json:"payload"`
	Generation domain.Generation `json:"generation"`
}

// DeviceCall records one scripted device attempt and its deterministic
// retry state. Failures never fabricate readings, consume samples or advance
// stages.
type DeviceCall struct {
	ID            string             `json:"id"`
	Device        string             `json:"device"`
	RequestDigest string             `json:"request_digest"`
	LogicalTime   domain.LogicalTime `json:"logical_time"`
	RetrySeq      int64              `json:"retry_seq"`
	Status        CallStatus         `json:"status"`
	Reading       domain.Fixed       `json:"reading,omitempty"`
	HasReading    bool               `json:"has_reading,omitempty"`
}

// CallStatus is the terminal or pending status of a device call.
type CallStatus string

const (
	CallPending   CallStatus = "pending"
	CallSucceeded CallStatus = "succeeded"
	CallRejected  CallStatus = "rejected"
	CallTimeout   CallStatus = "timeout"
	CallMalformed CallStatus = "malformed"
)

// CutCell is an integer-micron cuboid within a body boundary.
type CutCell struct {
	Row     int   `json:"row"`
	Column  int   `json:"column"`
	Layer   int   `json:"layer"`
	X, Y, Z int64 `json:"-"`
	Width   int64 `json:"width_um"`
	Height  int64 `json:"height_um"`
	Depth   int64 `json:"depth_um"`
	Notch   int64 `json:"notch_um"`  // non-negative
	Margin  int64 `json:"margin_um"` // non-negative cutting allowance
}

// SamplingMap maps blocks to sample candidates and offcuts and tracks sample
// consumption.
type SamplingMap struct {
	BlockToSizeSample     map[string]string `json:"block_to_size_sample"`
	BlockToDensitySample  map[string]string `json:"block_to_density_sample"`
	BlockToStrengthSample map[string]string `json:"block_to_strength_sample"`
	BlockToFreezeSample   map[string]string `json:"block_to_freeze_sample"`
	BlockToOffcut         map[string]string `json:"block_to_offcut"`
	Consumed              map[string]bool   `json:"consumed"`
}

// NewSamplingMap returns an empty sampling map.
func NewSamplingMap() *SamplingMap {
	return &SamplingMap{
		BlockToSizeSample:     make(map[string]string),
		BlockToDensitySample:  make(map[string]string),
		BlockToStrengthSample: make(map[string]string),
		BlockToFreezeSample:   make(map[string]string),
		BlockToOffcut:         make(map[string]string),
		Consumed:              make(map[string]bool),
	}
}
