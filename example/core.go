package main

import (
	"context"
	"fmt"
	"time"

	"github.com/xfrr/assemble"
)

type Logger interface {
	Infof(msg string, args ...any)
}

type ZapLogger struct{}

func NewZapLogger() (*ZapLogger, error) { return &ZapLogger{}, nil }
func (z *ZapLogger) Infof(msg string, args ...any) {
	fmt.Printf(time.Now().Format(time.RFC3339)+" | "+msg+"\n", args...)
}

type Middleware interface {
	Name() string
}

type authMW struct{ l Logger }

func (a authMW) Name() string { return "auth" }

func NewAuthMW(r assemble.Resolver) (Middleware, error) {
	l, _ := assemble.Get[Logger](r)
	l.Infof("init auth middleware")
	return authMW{l: l}, nil
}

type recoverMW struct{}

func (r recoverMW) Name() string                           { return "recover" }
func NewRecoverMW(_ assemble.Resolver) (Middleware, error) { return recoverMW{}, nil }

type Server struct {
	log Logger
	mw  []Middleware
}

func NewServer(r assemble.Resolver) (*Server, error) {
	log, err := assemble.Get[Logger](r)
	if err != nil {
		return nil, err
	}
	mw, err := assemble.Get[[]Middleware](r)
	if err != nil {
		return nil, err
	}
	return &Server{log: log, mw: mw}, nil
}

func StartHTTP(r assemble.Resolver) error {
	srv, err := assemble.Get[*Server](r)
	if err != nil {
		return err
	}
	var names []string
	for _, m := range srv.mw {
		names = append(names, m.Name())
	}
	srv.log.Infof("HTTP server starting with middlewares: %v", names)
	return nil
}

var Core = assemble.Module{
	// Provide Logger using inline constructor
	assemble.Provide(func(_ assemble.Resolver) (Logger, error) {
		z, err := NewZapLogger()
		if err != nil {
			return nil, err
		}
		return Logger(z), nil
	}),
	// Multibinding set of Middlewares
	assemble.Set(
		assemble.Append(NewAuthMW),
		assemble.Append(NewRecoverMW),
	),
	// Provide Server using constructor function
	assemble.Provide(NewServer),
	// Generic start hook (starts HTTP server)
	assemble.Invoke(StartHTTP),
	// Generic stop hook (logs shutdown)
	assemble.OnStop(func(_ context.Context, r assemble.Resolver) error {
		srv, err := assemble.Get[*Server](r)
		if err == nil {
			srv.log.Infof("HTTP server stopping...")
		}
		return nil
	}),
	// Stop hook with dependencies: ensure Server stops before its dependencies.
	assemble.OnStopFor(func(_ context.Context, r assemble.Resolver, srv *Server) error {
		// do graceful shutdown...
		srv.log.Infof("Server: graceful shutdown...")
		// time.Sleep(100 * time.Millisecond) // simulate
		return nil
	}, assemble.WithStopTimeout(5*time.Second)),
}
