package evidence

import (
	"sort"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// AttemptKind is the outcome of a scripted device attempt.
type AttemptKind string

const (
	AttemptRejected  AttemptKind = "rejected"
	AttemptTimeout   AttemptKind = "timeout"
	AttemptMalformed AttemptKind = "malformed"
	AttemptSuccess   AttemptKind = "success"
)

// DeviceRegistry owns the deterministic retry lifecycle of scripted device
// calls. Rejected, timed-out or malformed attempts never fabricate a reading,
// consume a sample or advance a stage; they only bump the deterministic retry
// sequence and leave the call pending. A successful attempt commits its reading
// exactly once.
type DeviceRegistry struct {
	calls map[string]DeviceCall
}

// NewDeviceRegistry returns an empty device-call registry.
func NewDeviceRegistry() *DeviceRegistry {
	return &DeviceRegistry{calls: make(map[string]DeviceCall)}
}

// Register adds a pending device call. Duplicate ids are rejected.
func (r *DeviceRegistry) Register(c DeviceCall) error {
	if _, ok := r.calls[c.ID]; ok {
		return domain.Newf(domain.CodeDuplicateBody, "duplicate device call %s", c.ID)
	}
	c.Status = CallPending
	r.calls[c.ID] = c
	return nil
}

// Get returns the current device call.
func (r *DeviceRegistry) Get(id string) (DeviceCall, bool) {
	c, ok := r.calls[id]
	return c, ok
}

// Record processes one scripted attempt against a pending call. Success commits
// the reading once and flips the call to succeeded; any other outcome leaves the
// call pending and returns a retryable DEVICE_RETRY_PENDING error with the
// deterministic incremented retry sequence.
func (r *DeviceRegistry) Record(id string, kind AttemptKind, reading domain.Fixed, t domain.LogicalTime) (DeviceCall, error) {
	c, ok := r.calls[id]
	if !ok {
		return DeviceCall{}, domain.Newf(domain.CodeInvalidArgument, "unknown device call %s", id)
	}
	if c.Status == CallSucceeded {
		// A successful call is terminal; further attempts are idempotent reads.
		return c, nil
	}
	c.RetrySeq++
	c.LogicalTime = t
	switch kind {
	case AttemptSuccess:
		c.Status = CallSucceeded
		c.Reading = reading
		c.HasReading = true
		r.calls[id] = c
		return c, nil
	default:
		// rejected, timeout, malformed: remain pending, no reading recorded.
		c.Status = CallPending
		c.HasReading = false
		r.calls[id] = c
		return c, domain.Newf(domain.CodeDeviceRetryPending, "device call %s pending after %s attempt (retry %d)", id, kind, c.RetrySeq).WithRetryable(true)
	}
}

// Calls returns all device calls in deterministic id order.
func (r *DeviceRegistry) Calls() []DeviceCall {
	out := make([]DeviceCall, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Load replaces the registry with a recovered snapshot.
func (r *DeviceRegistry) Load(calls []DeviceCall) {
	r.calls = make(map[string]DeviceCall, len(calls))
	for _, c := range calls {
		r.calls[c.ID] = c
	}
}
