package client

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestPingWithRetry_ImmediateSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	if err := pingWithRetry(c); err != nil {
		t.Fatalf("pingWithRetry failed on immediate success: %v", err)
	}
}

func TestPingWithRetry_SucceedsAfterFailures(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	if err := pingWithRetry(c); err != nil {
		t.Fatalf("pingWithRetry failed: %v (calls=%d)", err, calls.Load())
	}
	if calls.Load() < 3 {
		t.Fatalf("expected at least 3 calls, got %d", calls.Load())
	}
}

func TestPingWithRetry_AllFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	err := pingWithRetry(c)
	if err == nil {
		t.Fatal("expected error when all pings fail")
	}
}
