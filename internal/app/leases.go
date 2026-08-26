package app

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
	"github.com/example/aac-block-masonry-admission-closure/internal/evidence"
	"github.com/example/aac-block-masonry-admission-closure/internal/mass"
)

// LeaseRequest requests an exclusive lease on a shared resource or position.
type LeaseRequest struct {
	TaskID       string             `json:"-"`
	ResourceType mass.ResourceType  `json:"resource_type"`
	ResourceID   string             `json:"resource_id"`
	Holder       string             `json:"holder"`
	LogicalTime  domain.LogicalTime `json:"logical_time"`
	Duration     domain.LogicalTime `json:"duration"`
}

// LeaseResponse is a granted or renewed lease.
type LeaseResponse struct {
	ResourceType mass.ResourceType  `json:"resource_type"`
	ResourceID   string             `json:"resource_id"`
	Token        domain.Token       `json:"token"`
	AcquiredAt   domain.LogicalTime `json:"acquired_at"`
	ExpiresAt    domain.LogicalTime `json:"expires_at"`
}

// AcquireLease grants an exclusive lease, persisting atomically.
func (s *Service) AcquireLease(req LeaseRequest) (LeaseResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(req.TaskID)
	if err != nil {
		return LeaseResponse{}, err
	}
	expiry := req.LogicalTime + req.Duration
	if expiry <= req.LogicalTime {
		return LeaseResponse{}, domain.New(domain.CodeLeaseExpired, "lease duration must be positive")
	}
	token := domain.Token(newToken())
	l, err := rt.Leases.Acquire(req.ResourceType, req.ResourceID, req.Holder, req.LogicalTime, expiry, token)
	if err != nil {
		return LeaseResponse{}, err
	}
	res := LeaseResponse{ResourceType: l.ResourceType, ResourceID: l.ResourceID, Token: l.Token, AcquiredAt: l.AcquiredAt, ExpiresAt: l.ExpiresAt}
	s.appendEvent(req.TaskID, "lease-acquire", "", res)
	if err := s.persist(); err != nil {
		return LeaseResponse{}, err
	}
	return res, nil
}

// RenewLease renews a live lease only on token match; an expired lease cannot
// be revived.
func (s *Service) RenewLease(taskID string, token domain.Token, now, newExpiry domain.LogicalTime) (LeaseResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(taskID)
	if err != nil {
		return LeaseResponse{}, err
	}
	for _, l := range rt.Leases.Leases() {
		if l.Token != token {
			continue
		}
		renewed, err := l.Renew(token, now, newExpiry)
		if err != nil {
			return LeaseResponse{}, err
		}
		// Re-insert the renewed lease.
		rt.Leases.Load(replaceLease(rt.Leases.Leases(), renewed))
		res := LeaseResponse{ResourceType: renewed.ResourceType, ResourceID: renewed.ResourceID, Token: renewed.Token, AcquiredAt: renewed.AcquiredAt, ExpiresAt: renewed.ExpiresAt}
		s.appendEvent(taskID, "lease-renew", "", res)
		if err := s.persist(); err != nil {
			return LeaseResponse{}, err
		}
		return res, nil
	}
	return LeaseResponse{}, domain.New(domain.CodeLeaseConflict, "unknown lease token")
}

func replaceLease(leases []mass.ResourceLease, renewed mass.ResourceLease) []mass.ResourceLease {
	out := make([]mass.ResourceLease, 0, len(leases))
	for _, l := range leases {
		if l.Token == renewed.Token {
			l = renewed
		}
		out = append(out, l)
	}
	return out
}

// DeviceAttemptRequest registers a scripted device attempt.
type DeviceAttemptRequest struct {
	TaskID      string               `json:"-"`
	CallID      string               `json:"-"`
	Kind        evidence.AttemptKind `json:"kind"`
	Reading     domain.Fixed         `json:"reading,omitempty"`
	LogicalTime domain.LogicalTime   `json:"logical_time"`
}

// DeviceCallRequest registers a pending device call.
type DeviceCallRequest struct {
	TaskID        string             `json:"-"`
	CallID        string             `json:"-"`
	Device        string             `json:"device"`
	RequestDigest string             `json:"request_digest"`
	LogicalTime   domain.LogicalTime `json:"logical_time"`
}

// RegisterDeviceCall creates a pending device call.
func (s *Service) RegisterDeviceCall(req DeviceCallRequest) (evidence.DeviceCall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(req.TaskID)
	if err != nil {
		return evidence.DeviceCall{}, err
	}
	c := evidence.DeviceCall{ID: req.CallID, Device: req.Device, RequestDigest: req.RequestDigest, LogicalTime: req.LogicalTime}
	if err := rt.Devices.Register(c); err != nil {
		return evidence.DeviceCall{}, err
	}
	s.appendEvent(req.TaskID, "device-call-register", "", c)
	if err := s.persist(); err != nil {
		return evidence.DeviceCall{}, err
	}
	return c, nil
}

// RecordDeviceAttempt processes one scripted device attempt.
func (s *Service) RecordDeviceAttempt(req DeviceAttemptRequest) (evidence.DeviceCall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, err := s.getTask(req.TaskID)
	if err != nil {
		return evidence.DeviceCall{}, err
	}
	c, err := rt.Devices.Record(req.CallID, req.Kind, req.Reading, req.LogicalTime)
	if err != nil {
		// Persist the pending state before returning the retryable error.
		s.appendEvent(req.TaskID, "device-attempt", "", c)
		_ = s.persist()
		return c, err
	}
	s.appendEvent(req.TaskID, "device-attempt", "", c)
	if err := s.persist(); err != nil {
		return evidence.DeviceCall{}, err
	}
	return c, nil
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
