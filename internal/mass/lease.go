package mass

import (
	"sort"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// ResourceType enumerates the shared, mutually-exclusive resources.
type ResourceType string

const (
	ResourceMold        ResourceType = "mold"
	ResourceMixer       ResourceType = "mixer"
	ResourceStandingBay ResourceType = "standing_bay"
	ResourceCutLine     ResourceType = "cutting_line"
	ResourceKilnCarPos  ResourceType = "kiln_car_position"
	ResourceAutoclave   ResourceType = "autoclave"
	ResourcePress       ResourceType = "press"
)

// ResourceLease is a time-limited, mutually-exclusive lease over a shared
// resource or position, keyed by holder, logical acquire/expiry times and an
// opaque token.
type ResourceLease struct {
	ResourceType ResourceType       `json:"resource_type"`
	ResourceID   string             `json:"resource_id"`
	Holder       string             `json:"holder"`
	Token        domain.Token       `json:"token"`
	AcquiredAt   domain.LogicalTime `json:"acquired_at"`
	ExpiresAt    domain.LogicalTime `json:"expires_at"`
}

// Expired reports whether the lease has passed its logical expiry. A lease is
// valid on [AcquiredAt, ExpiresAt) and expires at ExpiresAt.
func (r ResourceLease) Expired(now domain.LogicalTime) bool {
	return !now.Before(r.ExpiresAt)
}

// Renew returns an extended lease only if the token matches and the lease has
// not already expired; an expired lease can never be revived.
func (r ResourceLease) Renew(token domain.Token, now, newExpiry domain.LogicalTime) (ResourceLease, error) {
	if r.Token != token {
		return r, domain.New(domain.CodeLeaseConflict, "lease token mismatch")
	}
	if r.Expired(now) {
		return r, domain.New(domain.CodeLeaseExpired, "lease already expired")
	}
	if !newExpiry.After(now) {
		return r, domain.New(domain.CodeLeaseExpired, "renewal expiry is not in the future")
	}
	r.ExpiresAt = newExpiry
	return r, nil
}

// Release returns a released lease only on token match.
func (r ResourceLease) Release(token domain.Token) error {
	if r.Token != token {
		return domain.New(domain.CodeLeaseConflict, "lease token mismatch")
	}
	return nil
}

// LeaseTable is a deterministic in-memory lease registry for a single task.
type LeaseTable struct {
	leases map[string]ResourceLease // key: resourceType + "\x00" + resourceID
}

// NewLeaseTable returns an empty lease table.
func NewLeaseTable() *LeaseTable {
	return &LeaseTable{leases: make(map[string]ResourceLease)}
}

func leaseKey(rt ResourceType, id string) string { return string(rt) + "\x00" + id }

// Acquire grants an exclusive lease, rejecting conflict with a live holder.
func (t *LeaseTable) Acquire(rt ResourceType, id, holder string, now, expiry domain.LogicalTime, token domain.Token) (ResourceLease, error) {
	if !expiry.After(now) {
		return ResourceLease{}, domain.New(domain.CodeLeaseExpired, "expiry is not in the future")
	}
	key := leaseKey(rt, id)
	if existing, ok := t.leases[key]; ok && !existing.Expired(now) {
		return existing, domain.Newf(domain.CodeLeaseConflict, "%s %s held until %d", rt, id, existing.ExpiresAt)
	}
	l := ResourceLease{
		ResourceType: rt, ResourceID: id, Holder: holder, Token: token,
		AcquiredAt: now, ExpiresAt: expiry,
	}
	t.leases[key] = l
	return l, nil
}

// Lookup returns the current lease for a resource, if any.
func (t *LeaseTable) Lookup(rt ResourceType, id string) (ResourceLease, bool) {
	l, ok := t.leases[leaseKey(rt, id)]
	return l, ok
}

// Leases returns all recorded leases in deterministic resource order, used for
// restart recovery snapshots.
func (t *LeaseTable) Leases() []ResourceLease {
	out := make([]ResourceLease, 0, len(t.leases))
	for _, l := range t.leases {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ResourceType != out[j].ResourceType {
			return out[i].ResourceType < out[j].ResourceType
		}
		return out[i].ResourceID < out[j].ResourceID
	})
	return out
}

// Load replaces the lease table with a recovered snapshot.
func (t *LeaseTable) Load(leases []ResourceLease) {
	t.leases = make(map[string]ResourceLease, len(leases))
	for _, l := range leases {
		t.leases[leaseKey(l.ResourceType, l.ResourceID)] = l
	}
}
