package assemble

import "context"

// Provider is a constructor that can resolve its dependencies via Resolve.
type Provider[T any] func(ctx context.Context, r Resolver) (T, error)

// Resolver resolves dependencies.
type Resolver interface {
	rawGet(ctx context.Context, k key) (any, error)
}

// Registrar mutates a module (registers providers, binds, etc.).
type Registrar interface {
	register(m *module)
}

// Modules flattens multiple modules into one.
func Modules(ms ...Module) Module {
	var out Module
	for _, m := range ms {
		out = append(out, m...)
	}
	return out
}

// Provide registers a provider of a concrete type T (usually a singleton).
func Provide[T any](p Provider[T], opts ...RegOpt) Registrar {
	return &provideReg[T]{p: p, opts: opts}
}

// Bind declares an interface binding to a concrete implementation.
func Bind[I any](as AsOpt, opts ...RegOpt) Registrar {
	return &bindReg{as: as, opts: opts}
}

// Set registers a multi-binding set for T, returned as []T on Get[[]T].
func Set[T any](appenders ...Appender[T]) Registrar {
	s := &setReg[T]{}
	for _, a := range appenders {
		a.apply(s)
	}
	return s
}

// Append adds an appender to a multi-binding set for T.
func Append[T any](p Provider[T], opts ...RegOpt) Appender[T] {
	return Appender[T]{p: p, opts: opts}
}

// Invoke registers an startup hook. The function may resolve deps via Resolve.
func Invoke(fn func(ctx context.Context, r Resolver) error) Registrar {
	return &invokeReg{fn: fn}
}

// Get resolves a dependency of type T from the Resolver r, applying optional KeyOpts.
func Get[T any](ctx context.Context, r Resolver, opts ...KeyOpt) (T, error) {
	k := keyFor[T](opts...)
	v, err := r.rawGet(ctx, k)
	if err != nil {
		var zero T
		return zero, err
	}

	typed, ok := v.(T)
	if !ok {
		var zero T
		return zero, BadCastError{Key: k, From: typeOf[T](), To: typeOf[T]()}
	}

	return typed, nil
}
