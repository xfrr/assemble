package assemble

import (
	"context"
	"time"
)

/* =========================
   Public API: Hook options
   ========================= */

type HookOpt interface {
	applyStart(*startHook)
	applyStop(*stopHook)
}

// WithStopTimeout sets a per-hook timeout applied by Container.Stop.
func WithStopTimeout(d time.Duration) HookOpt { return stopTimeoutOpt{d: d} }

// WithStartTimeout sets a per-hook timeout applied by Container.Start.
func WithStartTimeout(d time.Duration) HookOpt { return startTimeoutOpt{d: d} }

// WithPriority assigns a group/priority:
//
//	Start: lower values start earlier
//	Stop:  higher values stop later (reverse order)
func WithPriority(p int) HookOpt { return priorityOpt{p: p} }

/* =========================
   Internal: Hooks model
   ========================= */

type hookFunc func(context.Context, Resolver) error

type stopHook struct {
	fn       hookFunc
	timeout  time.Duration
	order    int
	orderFn  func(*Container) int // computed at Stop time (e.g., by creation index)
	priority int                  // higher stops later (Stop sorts DESC)
}

type startHook struct {
	fn       hookFunc
	timeout  time.Duration
	order    int
	orderFn  func(*Container) int // computed at Start time if needed
	priority int                  // lower starts earlier (Start sorts ASC)
}

// Timeouts
type stopTimeoutOpt struct{ d time.Duration }
type startTimeoutOpt struct{ d time.Duration }

func (o stopTimeoutOpt) applyStop(h *stopHook)    { h.timeout = o.d }
func (o stopTimeoutOpt) applyStart(_ *startHook)  {}
func (o startTimeoutOpt) applyStart(h *startHook) { h.timeout = o.d }
func (o startTimeoutOpt) applyStop(_ *stopHook)   {}

// Priority / Grouping
type priorityOpt struct{ p int }

func (o priorityOpt) applyStart(h *startHook) { h.priority = o.p }
func (o priorityOpt) applyStop(h *stopHook)   { h.priority = o.p }

/* =========================
   Registration helpers
   ========================= */

// OnStop registers a shutdown hook executed by Container.Stop(ctx).
// Ordering: priority DESC, then registration/order DESC (reverse).
func OnStop(fn func(ctx context.Context, r Resolver) error, opts ...HookOpt) Registrar {
	h := stopHook{fn: wrapOrNoop(fn)}
	applyStopOpts(&h, opts)
	return stopReg(h)
}

// OnStopFor registers a shutdown hook tied to a specific type T.
// The hook runs after dependents of T, by ordering using T's creation index
// (instances created later stop earlier).
func OnStopFor[T any](fn func(ctx context.Context, r Resolver, t T) error, opts ...HookOpt) Registrar {
	h := stopHook{
		fn: typedHook(fn),
		// Compute order from the creation index of key T at Stop time.
		orderFn: orderByCreationIndex[T](),
	}
	applyStopOpts(&h, opts)
	return stopReg(h)
}

// OnStart registers a startup hook executed by Container.Start(ctx).
// Ordering: priority ASC, then registration/order ASC.
func OnStart(fn func(ctx context.Context, r Resolver) error, opts ...HookOpt) Registrar {
	h := startHook{fn: wrapOrNoop(fn)}
	applyStartOpts(&h, opts)
	return startReg(h)
}

// OnStartFor resolves T and passes it to the hook; useful to pre-warm or ping services.
func OnStartFor[T any](fn func(ctx context.Context, r Resolver, t T) error, opts ...HookOpt) Registrar {
	h := startHook{fn: typedHook(fn)}
	applyStartOpts(&h, opts)
	return startReg(h)
}

/* =========================
   Registrar adapters
   ========================= */

type stopReg stopHook

func (r stopReg) register(m *module) { m.addStop(stopHook(r)) }

type startReg startHook

func (r startReg) register(m *module) { m.addStart(startHook(r)) }

// invokeReg keeps legacy Invoke() support by wrapping into OnStart with default options.
type invokeReg struct {
	fn func(ctx context.Context, r Resolver) error
}

func (r *invokeReg) register(m *module) {
	m.addStart(startHook{
		fn: wrapOrNoop(r.fn), // default priority 0, no timeout
	})
}

/* =========================
   Small utilities
   ========================= */

func applyStopOpts(h *stopHook, opts []HookOpt) {
	for _, o := range opts {
		o.applyStop(h)
	}
}

func applyStartOpts(h *startHook, opts []HookOpt) {
	for _, o := range opts {
		o.applyStart(h)
	}
}

// typedHook wraps a T-typed hook into the generic hookFunc form.
func typedHook[T any](fn func(ctx context.Context, r Resolver, t T) error) hookFunc {
	if fn == nil {
		return noopHook
	}
	return func(ctx context.Context, r Resolver) error {
		t, err := Get[T](ctx, r)
		if err != nil {
			return err
		}
		return fn(ctx, r, t)
	}
}

// orderByCreationIndex returns an order function that uses T's creation index.
// If T wasn't created/cached, it returns -1 so it stops first.
func orderByCreationIndex[T any]() func(*Container) int {
	return func(c *Container) int {
		k := keyFor[T]()
		c.mu.RLock()
		defer c.mu.RUnlock()
		if idx, ok := c.creationIndex[k]; ok {
			return idx
		}
		return -1
	}
}

// wrapOrNoop guards against nil hook functions to avoid panics.
func wrapOrNoop(fn func(ctx context.Context, r Resolver) error) hookFunc {
	if fn == nil {
		return noopHook
	}
	return fn
}

func noopHook(context.Context, Resolver) error { return nil }
