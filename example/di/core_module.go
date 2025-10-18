package di

import (
	"context"
	"time"

	"github.com/xfrr/assemble"
)

var Core = assemble.Module{
	// Provide Logger using inline constructor
	assemble.Provide(func(_ context.Context, _ assemble.Resolver) (SimpleLogger, error) {
		z, err := NewSimpleLogger()
		if err != nil {
			return SimpleLogger{}, err
		}
		return z, nil
	}),
	// Provide InMemoryRepo using constructor function
	assemble.Provide(func(_ context.Context, _ assemble.Resolver) (*InMemoryRepo, error) {
		return NewInMemoryRepo()
	}),
	// Bind Repository interface to InMemoryRepo implementation
	assemble.Bind[Repository](assemble.As[*InMemoryRepo]()),
	// Provide Server using constructor function
	assemble.Provide(NewServer),
	// Generic start hook (starts HTTP server)
	assemble.Invoke(StartServer),
	// Generic stop hook (logs shutdown)
	assemble.OnStop(func(ctx context.Context, r assemble.Resolver) error {
		logger, err := assemble.Get[SimpleLogger](ctx, r)
		if err != nil {
			return err
		}
		logger.Infof("Core module: shutdown complete.")
		return nil
	}),
	// Stop hook with dependencies: ensure Server stops before its dependencies.
	assemble.OnStopFor(func(_ context.Context, _ assemble.Resolver, srv *Server) error {
		// do graceful shutdown...
		srv.Logger.Infof("Server: graceful shutdown...")
		// time.Sleep(100 * time.Millisecond) // simulate
		return nil
	}, assemble.WithStopTimeout(5*time.Second)),
}
