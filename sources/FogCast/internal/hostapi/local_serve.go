package hostapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
)

// LocalServe is a loopback host API in front of BootLocalCatalog.
// The listener is 127.0.0.1 only. Close stops the listener and the service.
type LocalServe struct {
	Base    string
	Notice  string
	service *fogcast.Service
	server  *http.Server
}

// ServeLocal boots the catalog and serves the existing host API on
// 127.0.0.1:0. Callers that only browse must still avoid treating a failed
// Health read as a reason to dial some other host.
func ServeLocal(ctx context.Context, paths fogcast.Paths) (*LocalServe, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service, notice, err := fogcast.BootLocalCatalog(ctx, paths)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = service.Close()
		return nil, err
	}
	srv := &http.Server{Handler: New(service), ReadHeaderTimeout: 2 * time.Second}
	failed := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			failed <- err
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	var dialErr error
	for {
		conn, err := net.DialTimeout("tcp", ln.Addr().String(), 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			dialErr = nil
			break
		}
		dialErr = err
		select {
		case err := <-failed:
			_ = service.Close()
			return nil, err
		default:
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = srv.Shutdown(shutdown)
			cancel()
			_ = service.Close()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, dialErr
		}
		time.Sleep(5 * time.Millisecond)
	}
	return &LocalServe{
		Base:    "http://" + ln.Addr().String(),
		Notice:  notice,
		service: service,
		server:  srv,
	}, nil
}

// Close shuts down the loopback listener and the catalog service.
func (l *LocalServe) Close() error {
	if l == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var serveErr error
	if l.server != nil {
		serveErr = l.server.Shutdown(ctx)
	}
	var serviceErr error
	if l.service != nil {
		serviceErr = l.service.Close()
	}
	if serveErr != nil {
		return serveErr
	}
	return serviceErr
}

// ContentPath resolves one catalog id to a local file. It does not dial.
func (l *LocalServe) ContentPath(ctx context.Context, gameID string) (string, error) {
	if l == nil || l.service == nil {
		return "", errors.New("local catalog is unavailable")
	}
	return l.service.LocalContentPath(ctx, gameID)
}
