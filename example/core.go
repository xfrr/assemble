package main

import (
	"context"
	"time"

	"github.com/xfrr/assemble"
	"github.com/xfrr/assemble/example/di"
)

type Server struct {
	logger di.SimpleLogger
	repo   di.Repository
}

func NewServer(ctx context.Context, resolver assemble.Resolver) (*Server, error) {
	logger, err := assemble.Get[di.SimpleLogger](ctx, resolver)
	if err != nil {
		return nil, err
	}
	repo, err := assemble.Get[di.Repository](ctx, resolver)
	if err != nil {
		return nil, err
	}
	return &Server{
		logger: logger,
		repo:   repo,
	}, nil
}

func StartServer(ctx context.Context, resolver assemble.Resolver) error {
	srv, err := assemble.Get[*Server](ctx, resolver)
	if err != nil {
		return err
	}

	srv.logger.Infof("Starting HTTP server...")
	// Simulate server start...
	// time.Sleep(100 * time.Millisecond)
	return nil
}

var Core = assemble.Module{
	// Provide Logger using inline constructor
	assemble.Provide(func(_ context.Context, _ assemble.Resolver) (di.SimpleLogger, error) {
		z, err := di.NewSimpleLogger()
		if err != nil {
			return di.SimpleLogger{}, err
		}
		return z, nil
	}),
	// Provide InMemoryRepo using constructor function
	assemble.Provide(func(_ context.Context, _ assemble.Resolver) (*di.InMemoryRepo, error) {
		return di.NewInMemoryRepo()
	}),
	// Bind Repository interface to InMemoryRepo implementation
	assemble.Bind[di.Repository](assemble.As[*di.InMemoryRepo]()),
	// Provide Server using constructor function
	assemble.Provide(NewServer),
	// Generic start hook (starts HTTP server)
	assemble.Invoke(StartServer),
	// Generic stop hook (logs shutdown)
	assemble.OnStop(func(ctx context.Context, r assemble.Resolver) error {
		srv, err := assemble.Get[*Server](ctx, r)
		if err == nil {
			srv.logger.Infof("HTTP server stopping...")
		}
		return nil
	}),
	// Stop hook with dependencies: ensure Server stops before its dependencies.
	assemble.OnStopFor(func(_ context.Context, _ assemble.Resolver, srv *Server) error {
		// do graceful shutdown...
		srv.logger.Infof("Server: graceful shutdown...")
		// time.Sleep(100 * time.Millisecond) // simulate
		return nil
	}, assemble.WithStopTimeout(5*time.Second)),
}
