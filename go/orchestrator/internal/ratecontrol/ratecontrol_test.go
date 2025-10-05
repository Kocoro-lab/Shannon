package ratecontrol

import "testing"

func TestDelayForLimit(t *testing.T) {
	limit := RateLimit{RPM: 30, TPM: 60000}
	d := delayForLimit(limit, 1000)
	if d.Milliseconds() <= 0 {
		t.Fatalf("expected positive delay, got %v", d)
	}
}

func TestCombineLimits(t *testing.T) {
	a := RateLimit{RPM: 30, TPM: 50000}
	b := RateLimit{RPM: 20, TPM: 100000}
	combined := CombineLimits(a, b)
	if combined.RPM != 20 {
		t.Fatalf("expected RPM 20, got %d", combined.RPM)
	}
	if combined.TPM != 50000 {
		t.Fatalf("expected TPM 50000, got %d", combined.TPM)
	}
}
