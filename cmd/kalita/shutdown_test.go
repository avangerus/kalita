package main

import (
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// testSignal is a synthetic os.Signal so gracefulServe can be driven in a
// unit test without sending a real SIGTERM/SIGINT to the test process —
// Windows in particular has no portable way to deliver POSIX signals to
// itself, which is exactly why gracefulServe takes a channel rather than
// wiring signal.Notify internally (see main.go).
type testSignal string

func (s testSignal) String() string { return string(s) }
func (s testSignal) Signal()        {}

// listenerFor binds an ephemeral loopback port so the test doesn't collide
// with anything else and doesn't need root/admin for a low port.
func listenerFor(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// TestGracefulServeDrainsInFlightRequest: a request already being handled
// when the stop signal fires must still complete successfully — the server
// must stop ACCEPTING new work, not abandon work in progress.
func TestGracefulServeDrainsInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var onStopCalled atomic.Bool

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	})}
	l := listenerFor(t)
	addr := l.Addr().String()

	stop := make(chan os.Signal, 1)
	done := make(chan struct{})
	go func() {
		gracefulServe(srv, func() error { return srv.Serve(l) }, 5*time.Second, stop, func() { onStopCalled.Store(true) })
		close(done)
	}()

	// fire off the in-flight request, wait for the handler to actually start
	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}

	// now signal shutdown WHILE the handler is still blocked mid-request
	stop <- testSignal("SIGTERM")
	// give Shutdown a moment to stop accepting new conns, then let the
	// in-flight handler finish
	time.Sleep(50 * time.Millisecond)
	close(release)

	select {
	case err := <-errCh:
		t.Fatalf("in-flight request must complete, got error: %v", err)
	case resp := <-respCh:
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("want 200 from the in-flight request, got %d", resp.StatusCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight request never completed — shutdown must drain, not abandon, it")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("gracefulServe never returned after the in-flight request drained")
	}
	if !onStopCalled.Load() {
		t.Fatal("onStop must run when a stop signal fires")
	}
}

// TestGracefulServeForcesCloseOnTimeout: a handler that outlives the drain
// timeout gets its connection forced closed rather than hanging the process
// forever.
func TestGracefulServeForcesCloseOnTimeout(t *testing.T) {
	started := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(5 * time.Second) // deliberately longer than the shutdown timeout below
	})}
	l := listenerFor(t)
	addr := l.Addr().String()

	stop := make(chan os.Signal, 1)
	done := make(chan struct{})
	go func() {
		gracefulServe(srv, func() error { return srv.Serve(l) }, 100*time.Millisecond, stop, nil)
		close(done)
	}()

	go func() { _, _ = http.Get("http://" + addr + "/") }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}

	stop <- testSignal("SIGTERM")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("gracefulServe must force-close and return promptly once the drain timeout elapses, not hang")
	}
}
