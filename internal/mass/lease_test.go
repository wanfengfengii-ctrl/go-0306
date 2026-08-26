package mass

import (
	"testing"

	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

func TestLeaseExpiry(t *testing.T) {
	l := ResourceLease{AcquiredAt: 0, ExpiresAt: 10}
	if l.Expired(5) {
		t.Fatal("lease should not be expired at 5")
	}
	if !l.Expired(10) {
		t.Fatal("lease should be expired at 10")
	}
}

func TestLeaseRenewTokenMismatch(t *testing.T) {
	l := ResourceLease{Token: "t1", AcquiredAt: 0, ExpiresAt: 10}
	if _, err := l.Renew("t2", 5, 20); err == nil {
		t.Fatal("expected token mismatch error")
	}
}

func TestLeaseRenewExpiredNotRevived(t *testing.T) {
	l := ResourceLease{Token: "t1", AcquiredAt: 0, ExpiresAt: 10}
	if _, err := l.Renew("t1", 15, 20); err == nil {
		t.Fatal("expected expired lease error")
	}
}

func TestLeaseTableConflict(t *testing.T) {
	tt := NewLeaseTable()
	if _, err := tt.Acquire(ResourceMixer, "m1", "op-a", 1, 100, "tok-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := tt.Acquire(ResourceMixer, "m1", "op-b", 2, 100, "tok-b"); err == nil {
		t.Fatal("expected lease conflict")
	}
}

func TestLeaseTableAcquireAfterExpiry(t *testing.T) {
	tt := NewLeaseTable()
	if _, err := tt.Acquire(ResourceMixer, "m1", "op-a", 1, 10, "tok-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := tt.Acquire(ResourceMixer, "m1", "op-b", 11, 100, "tok-b"); err != nil {
		t.Fatalf("expected acquire after expiry, got %v", err)
	}
}

func TestLeaseTableRenewLifecycle(t *testing.T) {
	tt := NewLeaseTable()
	lease, err := tt.Acquire(ResourceMold, "md1", "op-a", 1, 10, "tok-a")
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := lease.Renew(domain.Token("tok-a"), 5, 20)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.ExpiresAt != 20 {
		t.Fatalf("expiry %d, want 20", renewed.ExpiresAt)
	}
}
