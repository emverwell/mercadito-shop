package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const shutdownCap = 30 * time.Second

// Run serves until ctx is cancelled, then stops. With drain == 0 the listener
// and open connections are closed at once. With drain > 0 readiness flips to
// 503 first, the server keeps serving for drain, then shuts down gracefully.
func Run(ctx context.Context, srv *http.Server, drain time.Duration, rd *Readiness, log *slog.Logger) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down", "drain", drain.String())
	if drain <= 0 {
		_ = srv.Close()
	} else {
		rd.SetDraining()
		time.Sleep(drain)
		sctx, cancel := context.WithTimeout(context.Background(), shutdownCap)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			return err
		}
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// NewServer returns an http.Server on :port. Only the header read is bounded;
// handlers may legitimately run for as long as a downstream timeout allows.
func NewServer(port string, h http.Handler) *http.Server {
	return &http.Server{Addr: ":" + port, Handler: h, ReadHeaderTimeout: 5 * time.Second}
}
