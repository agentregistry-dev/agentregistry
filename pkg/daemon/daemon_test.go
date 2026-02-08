package daemon

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func TestDefaultDaemonManagerImplementsInterface(t *testing.T) {
	var _ types.DaemonManager = (*DefaultDaemonManager)(nil)
}

func TestWaitForReady_AlreadyReady(t *testing.T) {
	// Start a test server on port 12121 that immediately responds
	listener, err := net.Listen("tcp", "127.0.0.1:12121")
	if err != nil {
		t.Skip("port 12121 already in use, skipping")
	}

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go srv.Serve(listener)
	defer srv.Close()

	dm := NewDaemonManager(nil)
	if err := dm.WaitForReady(); err != nil {
		t.Fatalf("WaitForReady failed when server was already ready: %v", err)
	}
}

func TestWaitForReady_BecomesReadyAfterDelay(t *testing.T) {
	var ready atomic.Bool

	// Start a test server that returns 503 until we set ready=true
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ready.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})

	listener, err := net.Listen("tcp", "127.0.0.1:12121")
	if err != nil {
		t.Skip("port 12121 already in use, skipping")
	}

	srv := &http.Server{Handler: handler}
	go srv.Serve(listener)
	defer srv.Close()

	// Make server ready after 2 seconds
	go func() {
		time.Sleep(2 * time.Second)
		ready.Store(true)
	}()

	dm := NewDaemonManager(nil)
	start := time.Now()
	if err := dm.WaitForReady(); err != nil {
		t.Fatalf("WaitForReady failed: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 2*time.Second {
		t.Fatalf("WaitForReady returned too quickly: %v", elapsed)
	}
}

func TestIsServerResponding(t *testing.T) {
	// When no server is running on 12121, isServerResponding should return false quickly
	// (This test may be flaky if something is actually running on 12121)
	listener, err := net.Listen("tcp", "127.0.0.1:12121")
	if err != nil {
		t.Skip("port 12121 already in use, skipping")
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.Listener = listener
	srv.Start()
	defer srv.Close()

	if !isServerResponding() {
		t.Fatal("isServerResponding returned false when server is running")
	}
}
