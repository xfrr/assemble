package assemble

import (
	"context"
	"time"
)

type HookOpt interface {
	applyStart(*startHook)
	applyStop(*stopHook)
}

type stopHook struct {
	fn       func(context.Context, Resolver) error
	timeout  time.Duration
	order    int
	orderFn  func(*Container) int // compute order at Stop time (e.g., by creation index)
	priority int                  // higher stops later (Stop sorts DESC)
}

type startHook struct {
	fn       func(context.Context, Resolver) error
	timeout  time.Duration
	order    int
	orderFn  func(*Container) int // compute order at Start time if needed
	priority int                  // lower starts earlier (Start sorts ASC)
}

// Timeouts
type stopTimeoutOpt struct{ d time.Duration }
type startTimeoutOpt struct{ d time.Duration }

func (o stopTimeoutOpt) applyStop(h *stopHook)    { h.timeout = o.d }
func (o stopTimeoutOpt) applyStart(_ *startHook)  {}
func (o startTimeoutOpt) applyStart(h *startHook) { h.timeout = o.d }
func (o startTimeoutOpt) applyStop(_ *stopHook)   {}

// WithStopTimeout sets a per-hook timeout applied by Container.Stop.
func WithStopTimeout(d time.Duration) HookOpt { return stopTimeoutOpt{d: d} }

// WithStartTimeout sets a per-hook timeout applied by Container.Start.
func WithStartTimeout(d time.Duration) HookOpt { return startTimeoutOpt{d: d} }

// Priority / Grouping
type priorityOpt struct{ p int }

func (o priorityOpt) applyStart(h *startHook) { h.priority = o.p }
func (o priorityOpt) applyStop(h *stopHook)   { h.priority = o.p }

// WithPriority assigns a group/priority:
//
//	Start:  lower values start earlier
//	Stop:   higher values stop later (reverse order)
func WithPriority(p int) HookOpt { return priorityOpt{p: p} }

// OnStop registers a shutdown hook executed by Container.Stop(ctx).
// Ordering: priority DESC, then registration/order DESC (reverse).
func OnStop(fn func(ctx context.Context, r Resolver) error, opts ...HookOpt) Registrar {
	h := stopHook{fn: fn}
	for _, o := range opts {
		o.applyStop(&h)
	}
	return stopReg(h)
}

// OnStopFor registers a shutdown hook tied to a specific type T.
// The hook runs after dependents of T, by ordering using T's creation index
// (instances created later stop earlier).
func OnStopFor[T any](fn func(ctx context.Context, r Resolver, t T) error, opts ...HookOpt) Registrar {
	var h stopHook
	h.fn = func(ctx context.Context, r Resolver) error {
		t, err := Get[T](r)
		if err != nil {
			return err
		}
		return fn(ctx, r, t)
	}
	// compute order from the creation index of key T at Stop time
	h.orderFn = func(c *Container) int {
		k := keyFor[T]()
		c.mu.RLock()
		defer c.mu.RUnlock()
		if idx, ok := c.creationIndex[k]; ok {
			return idx
		}
		// if T wasn't created/cached, treat as earliest (stop first)
		return -1
	}
	for _, o := range opts {
		o.applyStop(&h)
	}
	return stopReg(h)
}

type stopReg stopHook

func (r stopReg) register(m *module) { m.addStop(stopHook(r)) }

// OnStart registers a startup hook executed by Container.Start(ctx).
// Ordering: priority ASC, then registration/order ASC.
func OnStart(fn func(ctx context.Context, r Resolver) error, opts ...HookOpt) Registrar {
	h := startHook{fn: fn}
	for _, o := range opts {
		o.applyStart(&h)
	}
	return startReg(h)
}

// OnStartFor resolves T and passes it to the hook; useful to pre-warm or ping services.
func OnStartFor[T any](fn func(ctx context.Context, r Resolver, t T) error, opts ...HookOpt) Registrar {
	var h startHook
	h.fn = func(ctx context.Context, r Resolver) error {
		t, err := Get[T](r)
		if err != nil {
			return err
		}
		return fn(ctx, r, t)
	}
	for _, o := range opts {
		o.applyStart(&h)
	}
	return startReg(h)
}

type startReg startHook

func (r startReg) register(m *module) { m.addStart(startHook(r)) }

// invokeReg keeps legacy Invoke() support by wrapping into OnStart with default options.
type invokeReg struct {
	fn func(r Resolver) error
}

func (r *invokeReg) register(m *module) {
	m.addStart(startHook{
		fn: func(_ context.Context, res Resolver) error { return r.fn(res) },
		// default priority 0, no timeout
	})
}
