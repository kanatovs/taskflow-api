package httpserver_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/platform/httpserver"
)

func TestServeWaitsForActiveRequest(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	stopping := make(chan struct{})
	server, listener := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
			_, _ = io.WriteString(w, "finished")
		case <-r.Context().Done():
		}
	}))
	server.RegisterOnShutdown(func() { close(stopping) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := serve(ctx, server, listener, 3*time.Second)
	response := sendRequest(listener)
	waitSignal(t, entered, "request start")

	cancel()
	waitSignal(t, stopping, "shutdown start")
	assertListenerClosed(t, listener)
	select {
	case err := <-result:
		t.Fatalf("Serve returned before the active request finished: %v", err)
	default:
	}
	close(release)
	select {
	case got := <-response:
		if got.err != nil || got.status != http.StatusOK || got.body != "finished" {
			t.Fatalf("active request interrupted: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("active request did not finish")
	}
	if err := waitResult(t, result); err != nil {
		t.Fatalf("Serve(): %v", err)
	}
}

func TestServeShutdownTimeoutClosesConnections(t *testing.T) {
	entered := make(chan struct{})
	requestCanceled := make(chan struct{})
	server, listener := newServer(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(requestCanceled)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := serve(ctx, server, listener, 20*time.Millisecond)
	response := sendRequest(listener)
	waitSignal(t, entered, "request start")
	cancel()

	if err := waitResult(t, result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Serve() = %v, want shutdown deadline error", err)
	}
	waitSignal(t, requestCanceled, "request cancellation after forced close")
	assertListenerClosed(t, listener)
	select {
	case got := <-response:
		if got.err == nil {
			t.Errorf("unfinished request unexpectedly succeeded: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client remained connected after shutdown timeout")
	}
}

func TestServeWithCanceledContext(t *testing.T) {
	server, listener := newServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitResult(t, serve(ctx, server, listener, time.Second)); err != nil {
		t.Fatalf("Serve() with canceled context: %v", err)
	}
	assertListenerClosed(t, listener)
}

func TestServeReturnsListenerFailure(t *testing.T) {
	server, listener := newServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	wantErr := errors.New("listener failed")
	listener = &failingListener{Listener: listener, err: wantErr}
	if err := waitResult(t, serve(context.Background(), server, listener, time.Second)); !errors.Is(err, wantErr) {
		t.Fatalf("Serve() = %v, want wrapped listener failure", err)
	}
	assertListenerClosed(t, listener)
}

func TestServeFailureClosesActiveConnection(t *testing.T) {
	entered := make(chan struct{})
	requestCanceled := make(chan struct{})
	server, listener := newServer(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(requestCanceled)
	}))
	wantErr := errors.New("listener failed after accepting a connection")
	listener = &failingListener{Listener: listener, err: wantErr, failAfter: entered}
	result := serve(context.Background(), server, listener, time.Second)
	response := sendRequest(listener)
	if err := waitResult(t, result); !errors.Is(err, wantErr) {
		t.Fatalf("Serve() = %v, want wrapped listener failure", err)
	}
	waitSignal(t, requestCanceled, "request cancellation after listener failure")
	select {
	case got := <-response:
		if got.err == nil {
			t.Errorf("request succeeded after listener failure: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client remained connected after listener failure")
	}
	assertListenerClosed(t, listener)
}

func newServer(t *testing.T, handler http.Handler) (*http.Server, net.Listener) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	t.Cleanup(func() {
		server.Close()
		listener.Close()
	})
	return server, listener
}

func serve(ctx context.Context, server *http.Server, listener net.Listener, timeout time.Duration) <-chan error {
	result := make(chan error, 1)
	go func() {
		result <- httpserver.Serve(ctx, server, listener, timeout, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	}()
	return result
}

type requestResult struct {
	status int
	body   string
	err    error
}

func sendRequest(listener net.Listener) <-chan requestResult {
	result := make(chan requestResult, 1)
	go func() {
		transport := &http.Transport{DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			result <- requestResult{err: err}
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		result <- requestResult{status: response.StatusCode, body: string(body), err: err}
	}()
	return result
}

func waitSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func waitResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Serve() did not return")
		return nil
	}
}

func assertListenerClosed(t *testing.T, listener net.Listener) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err == nil {
		connection.Close()
		t.Error("server still accepts connections during shutdown")
	}
}

type failingListener struct {
	net.Listener
	err       error
	failAfter <-chan struct{}
	accepted  bool
}

func (l *failingListener) Accept() (net.Conn, error) {
	if l.failAfter != nil {
		if !l.accepted {
			l.accepted = true
			return l.Listener.Accept()
		}
		<-l.failAfter
	}
	return nil, l.err
}
