package beaconcli

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testLifecycleLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBackgroundGroupCancelsOnWorkerErrorAndWaits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bg := newBackgroundGroup(ctx, cancel, testLifecycleLogger())
	workerErr := errors.New("worker failed")
	stopped := make(chan struct{})

	bg.Go("failing worker", func(context.Context) error {
		return workerErr
	})
	bg.Go("blocking worker", func(ctx context.Context) error {
		defer close(stopped)
		<-ctx.Done()
		return nil
	})

	err := bg.Wait()
	if !errors.Is(err, workerErr) || !strings.Contains(err.Error(), "failing worker") {
		t.Fatalf("Wait error = %v, want named worker error", err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("blocking worker did not stop before Wait returned")
	}
}

func TestRunWatchServicesCancelsBatcherAfterWatcherStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bg := newBackgroundGroup(ctx, cancel, testLifecycleLogger())
	batcherDone := make(chan struct{})

	err := runWatchServices(bg,
		func(ctx context.Context) {
			defer close(batcherDone)
			<-ctx.Done()
		},
		func(context.Context) error {
			cancel()
			return context.Canceled
		},
	)
	if err != nil {
		t.Fatalf("runWatchServices error = %v", err)
	}
	select {
	case <-batcherDone:
	case <-time.After(time.Second):
		t.Fatal("batcher was not stopped")
	}
}

func TestRunWatchServicesReturnsWatcherErrorAndStopsBatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bg := newBackgroundGroup(ctx, cancel, testLifecycleLogger())
	watcherErr := errors.New("watch failed")
	batcherDone := make(chan struct{})

	err := runWatchServices(bg,
		func(ctx context.Context) {
			defer close(batcherDone)
			<-ctx.Done()
		},
		func(context.Context) error {
			return watcherErr
		},
	)
	if !errors.Is(err, watcherErr) {
		t.Fatalf("runWatchServices error = %v, want %v", err, watcherErr)
	}
	select {
	case <-batcherDone:
	case <-time.After(time.Second):
		t.Fatal("batcher was not stopped after watcher error")
	}
}

func TestCommandLifecycleErrorTreatsContextCancellationAsClean(t *testing.T) {
	backgroundErr := errors.New("background failed")
	if got := commandLifecycleError(context.Canceled, nil); got != nil {
		t.Fatalf("context cancellation error = %v, want nil", got)
	}
	if got := commandLifecycleError(context.Canceled, backgroundErr); !errors.Is(got, backgroundErr) {
		t.Fatalf("context cancellation with background error = %v, want %v", got, backgroundErr)
	}
	runErr := errors.New("run failed")
	if got := commandLifecycleError(runErr, backgroundErr); !errors.Is(got, runErr) {
		t.Fatalf("run error = %v, want %v", got, runErr)
	}
}

func TestShutdownHTTPServerOnContextStopsServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}
	t.Cleanup(func() { _ = srv.Close() })
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
	}()

	resp, err := http.Get("http://" + ln.Addr().String())
	if err != nil {
		t.Fatalf("pre-shutdown request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- shutdownHTTPServerOnContext(ctx, srv, time.Second)
	}()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdownHTTPServerOnContext: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for shutdown")
	}

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve error = %v, want ErrServerClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}
