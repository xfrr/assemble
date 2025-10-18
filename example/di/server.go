package di

import (
	"context"

	"github.com/xfrr/assemble"
)

type Server struct {
	Logger SimpleLogger
	Repo   Repository
}

func NewServer(ctx context.Context, resolver assemble.Resolver) (*Server, error) {
	logger, err := assemble.Get[SimpleLogger](ctx, resolver)
	if err != nil {
		return nil, err
	}
	repo, err := assemble.Get[Repository](ctx, resolver)
	if err != nil {
		return nil, err
	}
	return &Server{
		Logger: logger,
		Repo:   repo,
	}, nil
}

func StartServer(ctx context.Context, resolver assemble.Resolver) error {
	srv, err := assemble.Get[*Server](ctx, resolver)
	if err != nil {
		return err
	}

	srv.Logger.Infof("Starting HTTP server...")
	// Simulate server start...
	// time.Sleep(100 * time.Millisecond)
	return nil
}
