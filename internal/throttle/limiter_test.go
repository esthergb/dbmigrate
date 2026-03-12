package throttle

import (
	"testing"
	"time"
)

func TestNewNil(t *testing.T) {
	l := New(0)
	if l != nil {
		t.Fatal("expected nil limiter for 0 rate")
	}
	l = New(-1)
	if l != nil {
		t.Fatal("expected nil limiter for negative rate")
	}
}

func TestNilWaitNoPanic(t *testing.T) {
	var l *Limiter
	l.Wait(100)
}

func TestLimiterThrottles(t *testing.T) {
	l := New(100)
	start := time.Now()
	l.Wait(100)
	l.Wait(100)
	elapsed := time.Since(start)
	if elapsed < 500*time.Millisecond {
		t.Fatalf("expected throttling delay, got %v", elapsed)
	}
}

func TestLimiterSmallBatch(t *testing.T) {
	l := New(10000)
	start := time.Now()
	l.Wait(5)
	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("small batch should not throttle, got %v", elapsed)
	}
}
