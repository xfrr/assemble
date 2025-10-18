package assemble

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"
)

const (
	defaultStartTimeout = 10 * time.Second
	defaultStopTimeout  = 10 * time.Second
)

// Container is the runtime DI container built from Modules.
type Container struct {
	mu    sync.RWMutex
	reg   *module
	cache map[Key]any

	started  bool
	stopping bool

	// creation order tracking for reverse dependency-aware shutdown
	creationIndex map[Key]int
	createSeq     int
}

// Assemble builds a Container from the provided Module(s).
func Assemble(ms ...Module) (*Container, error) {
	mod := newModule()
	for _, m := range Modules(ms...).clone() {
		m.register(mod)
	}
	return &Container{
		reg:           mod,
		cache:         make(map[Key]any),
		creationIndex: make(map[Key]int),
	}, nil
}

// Start runs all OnStart/Invoke hooks with timeout & multi-error aggregation.
func (c *Container) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	starts := copyStartHooks(c.reg.starts)
	c.mu.Unlock()

	assignOrders(starts, c)
	sortStarts(starts)

	var merr MultiError
	runHooksWithTimeout(ctx, starts, defaultStartTimeout, func(err error) error {
		// Keep ErrInvokeFail for backwards compatibility while aggregating.
		return fmt.Errorf("%w: %s", ErrInvokeFail, err.Error())
	}, &merr, c.newResolver())

	if merr.Len() > 0 {
		return &merr
	}

	c.mu.Lock()
	c.started = true
	c.mu.Unlock()
	return nil
}

// Stop runs all OnStop hooks honoring priority and registration/creation order.
func (c *Container) Stop(ctx context.Context) error {
	c.mu.Lock()
	if c.stopping {
		c.mu.Unlock()
		return nil
	}
	c.stopping = true
	stops := copyStopHooks(c.reg.stops)
	c.mu.Unlock()

	assignOrders(stops, c)
	sortStops(stops)

	var merr MultiError
	runHooksWithTimeout(ctx, stops, defaultStopTimeout, func(err error) error {
		// For Stop we keep the original errors (no wrapping).
		return err
	}, &merr, c.newResolver())

	if merr.Len() > 0 {
		return &merr
	}
	return nil
}

// Shutdown runs Stop(ctx) and returns a formatted error tree (if any).
func (c *Container) Shutdown(ctx context.Context) error {
	if err := c.Stop(ctx); err != nil {
		if me, ok := IsMultiError(err); ok {
			return fmt.Errorf("shutdown errors:\n%s", me.TreeString("  "))
		}
		return fmt.Errorf("shutdown error: %w", err)
	}
	return nil
}

// rawGet is the internal, non-generic resolver used by di.Get[T].
func (c *Container) rawGet(ctx context.Context, k Key) (any, error) {
	// fast path: cached
	if v, ok := c.lookupCache(k); ok {
		return v, nil
	}

	// direct providers
	if v, err := c.resolveDirect(ctx, k); err == nil {
		c.cacheIfAbsent(k, v)
		return v, nil
	} else if !isNotFound(err) {
		return nil, err
	}

	// interface bindings
	if k.sliceElem == nil && k.typ.Kind() == reflect.Interface {
		v, err := c.resolveViaBind(ctx, k)
		if err == nil {
			c.cacheIfAbsent(k, v)
			return v, nil
		}
		return nil, err
	}

	return nil, NotFoundError{Type: k.typ, Name: k.name}
}

func (c *Container) resolveDirect(ctx context.Context, k Key) (any, error) {
	fns, ok := c.reg.providers[k]
	if !ok || len(fns) == 0 {
		return nil, NotFoundError{Type: k.typ, Name: k.name}
	}

	res := c.newResolver()

	// sets: execute all providers and aggregate
	if k.sliceElem != nil {
		slice := reflect.MakeSlice(k.typ, 0, len(fns))
		for _, fn := range fns {
			val, err := fn(ctx, res)
			if err != nil {
				return nil, err
			}
			elem := reflect.ValueOf(val)
			if !elem.IsValid() || !elem.Type().AssignableTo(k.sliceElem) {
				// When invalid, elem.Type() is zero; protect against panic by checking IsValid above.
				return nil, BadCastError{Key: k, From: elem.Type(), To: k.sliceElem}
			}
			slice = reflect.Append(slice, elem)
		}
		return slice.Interface(), nil
	}

	// non-set: last registered wins (allow overrides in tests)
	val, err := fns[len(fns)-1](ctx, res)
	if err != nil {
		return nil, err
	}
	return val, nil
}

// resolveViaBind tries to find a concrete type for interface lookups.
func (c *Container) resolveViaBind(ctx context.Context, k Key) (any, error) {
	for pk := range c.reg.providers {
		if pk.sliceElem != nil {
			continue // sets are not candidates for interface binding
		}
		if pk.typ == nil || pk.typ.Kind() == reflect.Interface {
			continue
		}
		if pk.typ.Implements(k.typ) {
			v, err := c.rawGet(ctx, pk)
			if err != nil {
				return nil, err
			}
			// Double-check runtime implementation (defensive in case of mismatched types).
			if !reflect.TypeOf(v).Implements(k.typ) {
				return nil, BindError{
					From: k.typ,
					To:   pk.typ,
					Why:  "implementation does not satisfy interface at runtime",
				}
			}
			return v, nil
		}
	}
	return nil, NotFoundError{Type: k.typ, Name: k.name}
}

/* =========================
   Internal helpers
   ========================= */

func (c *Container) newResolver() *resolver { return &resolver{c: c} }

func (c *Container) lookupCache(k Key) (any, bool) {
	c.mu.RLock()
	v, ok := c.cache[k]
	c.mu.RUnlock()
	return v, ok
}

func (c *Container) cacheIfAbsent(k Key, v any) {
	c.mu.Lock()
	if _, exists := c.cache[k]; !exists {
		c.cache[k] = v
		c.creationIndex[k] = c.createSeq
		c.createSeq++
	}
	c.mu.Unlock()
}

type orderedHook interface {
	setOrder(int)
	getOrder() int
	getPriority() int
	getTimeout() time.Duration
	run(context.Context, *resolver) error
	deriveOrder(*Container, int) int
}

func assignOrders[T orderedHook](hooks []T, c *Container) {
	for i := range hooks {
		hooks[i].setOrder(hooks[i].deriveOrder(c, i))
	}
}

func sortStarts[T orderedHook](hooks []T) {
	sort.Slice(hooks, func(i, j int) bool {
		if hooks[i].getPriority() == hooks[j].getPriority() {
			return hooks[i].getOrder() < hooks[j].getOrder()
		}
		return hooks[i].getPriority() < hooks[j].getPriority()
	})
}

func sortStops[T orderedHook](hooks []T) {
	sort.Slice(hooks, func(i, j int) bool {
		if hooks[i].getPriority() == hooks[j].getPriority() {
			return hooks[i].getOrder() > hooks[j].getOrder()
		}
		return hooks[i].getPriority() > hooks[j].getPriority()
	})
}

func runHooksWithTimeout[T orderedHook](
	ctx context.Context,
	hooks []T,
	defaultTO time.Duration,
	wrap func(error) error,
	merr *MultiError,
	res *resolver,
) {
	for _, h := range hooks {
		to := h.getTimeout()
		if to <= 0 {
			to = defaultTO
		}
		hctx, cancel := context.WithTimeout(ctx, to)
		if err := h.run(hctx, res); err != nil {
			merr.Append(wrap(err))
		}
		cancel()
	}
}

/* =========================
   Adapters for start/stop hooks
   ========================= */

func copyStartHooks(src []startHook) []*startHook {
	dst := make([]*startHook, len(src))
	for i := range src {
		dst[i] = &src[i]
	}
	return dst
}

func copyStopHooks(src []stopHook) []*stopHook {
	dst := make([]*stopHook, len(src))
	for i := range src {
		dst[i] = &src[i]
	}
	return dst
}

func (h *startHook) setOrder(v int)            { h.order = v }
func (h *startHook) getOrder() int             { return h.order }
func (h *startHook) getPriority() int          { return h.priority }
func (h *startHook) getTimeout() time.Duration { return h.timeout }
func (h *startHook) run(ctx context.Context, r *resolver) error {
	return h.fn(ctx, r)
}
func (h *startHook) deriveOrder(c *Container, idx int) int {
	if h.orderFn != nil {
		return h.orderFn(c)
	}
	return idx
}

func (h *stopHook) setOrder(v int)            { h.order = v }
func (h *stopHook) getOrder() int             { return h.order }
func (h *stopHook) getPriority() int          { return h.priority }
func (h *stopHook) getTimeout() time.Duration { return h.timeout }
func (h *stopHook) run(ctx context.Context, r *resolver) error {
	return h.fn(ctx, r)
}
func (h *stopHook) deriveOrder(c *Container, idx int) int {
	if h.orderFn != nil {
		return h.orderFn(c)
	}
	return idx
}
