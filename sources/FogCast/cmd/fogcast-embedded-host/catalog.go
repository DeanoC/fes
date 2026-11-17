//go:build !wasip1 && !tamago

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
)

// Only the fixture owns this listener. The service and HTTP handlers are the
// same ones used by fogcast-api; no former local-host API is reintroduced.
type catalogHost struct {
	Base    string
	service *fogcast.Service
	server  *http.Server
	errors  chan error
}

func serveCatalog(ctx context.Context, paths fogcast.Paths) (*catalogHost, error) {
	service, err := fogcast.Open(ctx, paths, nil)
	if err != nil {
		return nil, err
	}
	if _, err = service.Scan(ctx); err != nil {
		return nil, errors.Join(err, service.Close())
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.Join(err, service.Close())
	}
	host := &catalogHost{Base: "http://" + listener.Addr().String(), service: service,
		server: &http.Server{Handler: hostapi.New(service), ReadHeaderTimeout: 2 * time.Second},
		errors: make(chan error, 1)}
	go func() {
		err := host.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		host.errors <- err
	}()
	return host, nil
}

func (host *catalogHost) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := host.server.Shutdown(ctx)
	if err != nil {
		err = errors.Join(err, host.server.Close())
	}
	return errors.Join(err, host.service.Close())
}
