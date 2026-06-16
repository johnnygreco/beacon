package beaconcli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

type backgroundGroup struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger *slog.Logger

	wg      sync.WaitGroup
	errOnce sync.Once
	err     error
}

func newBackgroundGroup(ctx context.Context, cancel context.CancelFunc, logger *slog.Logger) *backgroundGroup {
	if logger == nil {
		logger = slog.Default()
	}
	return &backgroundGroup{ctx: ctx, cancel: cancel, logger: logger}
}

// Go starts an owned background worker. The worker must return when ctx is
// cancelled; the group cancels the shared context and records the first
// non-cancellation error.
func (g *backgroundGroup) Go(name string, run func(context.Context) error) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if err := run(g.ctx); err != nil && !errors.Is(err, context.Canceled) {
			wrapped := fmt.Errorf("%s: %w", name, err)
			g.errOnce.Do(func() {
				g.err = wrapped
				g.logger.Error("background worker failed", "worker", name, "error", err)
				g.cancel()
			})
		}
	}()
}

func (g *backgroundGroup) Wait() error {
	g.wg.Wait()
	return g.err
}

func signalCancelWorker(sigCh <-chan os.Signal, cancel context.CancelFunc, logger *slog.Logger, message string) func(context.Context) error {
	return func(ctx context.Context) error {
		select {
		case sig := <-sigCh:
			if logger != nil {
				logger.Info(message, "signal", sig)
			}
			cancel()
		case <-ctx.Done():
		}
		return nil
	}
}

func shutdownHTTPServerOnContext(ctx context.Context, srv *http.Server, timeout time.Duration) error {
	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), timeout)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runWatchServices(bg *backgroundGroup, runBatcher func(context.Context), runWatcher func(context.Context) error) error {
	bg.Go("capture batcher", func(ctx context.Context) error {
		runBatcher(ctx)
		return nil
	})

	watcherErr := runWatcher(bg.ctx)
	bg.cancel()
	bgErr := bg.Wait()
	return commandLifecycleError(watcherErr, bgErr)
}

func commandLifecycleError(runErr, bgErr error) error {
	if runErr == nil || errors.Is(runErr, context.Canceled) {
		return bgErr
	}
	return runErr
}
