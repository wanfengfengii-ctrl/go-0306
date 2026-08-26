package evidence

import "testing"

func TestDeviceCallRetrySequence(t *testing.T) {
	r := NewDeviceRegistry()
	if err := r.Register(DeviceCall{ID: "c1", Device: "scale"}); err != nil {
		t.Fatal(err)
	}
	// rejected: pending, retry 1
	if _, err := r.Record("c1", AttemptRejected, fx(0), 1); err == nil {
		t.Fatal("expected pending error on reject")
	}
	c, _ := r.Get("c1")
	if c.RetrySeq != 1 || c.Status != CallPending {
		t.Fatalf("after reject: retry=%d status=%s", c.RetrySeq, c.Status)
	}
	if c.HasReading {
		t.Fatal("rejected attempt must not record a reading")
	}
	// timeout: retry 2
	if _, err := r.Record("c1", AttemptTimeout, fx(0), 2); err == nil {
		t.Fatal("expected pending error on timeout")
	}
	// malformed: retry 3
	if _, err := r.Record("c1", AttemptMalformed, fx(0), 3); err == nil {
		t.Fatal("expected pending error on malformed")
	}
	// success: commits reading once
	if _, err := r.Record("c1", AttemptSuccess, fx(1500), 4); err != nil {
		t.Fatal(err)
	}
	c, _ = r.Get("c1")
	if c.RetrySeq != 4 || c.Status != CallSucceeded || !c.HasReading {
		t.Fatalf("after success: %+v", c)
	}
	// further attempts are idempotent reads
	if _, err := r.Record("c1", AttemptSuccess, fx(9999), 5); err != nil {
		t.Fatal(err)
	}
	c, _ = r.Get("c1")
	if c.Reading.Scaled() != 1500 {
		t.Fatalf("reading changed to %d", c.Reading.Scaled())
	}
}
