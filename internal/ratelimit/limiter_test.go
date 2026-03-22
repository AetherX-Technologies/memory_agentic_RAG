package ratelimit

import (
	"testing"
)

func TestAllow_UnderLimit(t *testing.T) {
	l := New(Config{PerMinute: 5, PerHour: 100})
	for i := 0; i < 5; i++ {
		if err := l.Allow(); err != nil {
			t.Fatalf("expected allow on call %d, got: %v", i, err)
		}
	}
}

func TestAllow_MinuteLimit(t *testing.T) {
	l := New(Config{PerMinute: 3, PerHour: 100})
	for i := 0; i < 3; i++ {
		l.Allow()
	}
	err := l.Allow()
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	rle, ok := err.(*RateLimitError)
	if !ok {
		t.Fatalf("expected RateLimitError, got %T", err)
	}
	if rle.RetryAfter <= 0 {
		t.Error("expected positive RetryAfter")
	}
}

func TestAllow_DefaultConfig(t *testing.T) {
	l := New(DefaultConfig())
	if err := l.Allow(); err != nil {
		t.Fatalf("first call should be allowed: %v", err)
	}
}
