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
	mu       sync.RWMutex
	reg      *module
	cache    map[key]any
	started  bool
	stopping bool

	// creation order tracking for reverse dependency-aware shutdown
	creationIndex map[key]int
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
		cache:         make(map[key]any),
		creationIndex: make(map[key]int),
	}, nil
}

// Start runs all OnStart/Invoke hooks with timeout & multi-error aggregation.
func (c *Container) Start(ctx context.Context) error {
	// Copy starts and release lock before executing user code to avoid deadlocks.
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	starts := make([]startHook, len(c.reg.starts))
	copy(starts, c.reg.starts)
	c.mu.Unlock()

	res := &resolver{c: c}

	// Start ordering: priority ASC, then order ASC (registration/orderFn).
	for i := range starts {
		if starts[i].orderFn != nil {
			starts[i].order = starts[i].orderFn(c)
		} else {
			starts[i].order = i
		}
	}
	sort.Slice(starts, func(i, j int) bool {
		if starts[i].priority == starts[j].priority {
			return starts[i].order < starts[j].order
		}
		return starts[i].priority < starts[j].priority
	})

	var merr MultiError
	for _, h := range starts {
		to := h.timeout
		if to <= 0 {
			to = defaultStartTimeout
		}
		hctx, cancel := context.WithTimeout(ctx, to)
		if err := h.fn(hctx, res); err != nil {
			// Keep ErrInvokeFail for backwards-compat signal, but aggregate.
			merr.Append(fmt.Errorf("%w: %s", ErrInvokeFail, err.Error()))
		}
		cancel()
	}
	if merr.Len() > 0 {
		return &merr
	}

	c.mu.Lock()
	c.started = true
	c.mu.Unlock()
	return nil
}

func (c *Container) Stop(ctx context.Context) error {
	c.mu.Lock()
	if c.stopping {
		c.mu.Unlock()
		return nil
	}
	c.stopping = true
	// Copy stops and release lock before executing user code to avoid deadlocks.
	stops := make([]stopHook, len(c.reg.stops))
	copy(stops, c.reg.stops)
	c.mu.Unlock()

	res := &resolver{c: c}

	// Compute order for each hook (higher = stop later). Default registration index.
	for i := range stops {
		if stops[i].orderFn != nil {
			stops[i].order = stops[i].orderFn(c)
		} else {
			stops[i].order = i
		}
	}
	// Stop ordering: priority DESC, then order DESC (reverse creation/registration).
	sort.Slice(stops, func(i, j int) bool {
		if stops[i].priority == stops[j].priority {
			return stops[i].order > stops[j].order
		}
		return stops[i].priority > stops[j].priority
	})

	var merr MultiError
	for _, h := range stops {
		to := h.timeout
		if to <= 0 {
			to = defaultStopTimeout
		}
		hctx, cancel := context.WithTimeout(ctx, to)
		if err := h.fn(hctx, res); err != nil {
			merr.Append(err)
		}
		cancel()
	}
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
func (c *Container) rawGet(k key) (any, error) {
	// fast path: cached
	c.mu.RLock()
	if v, ok := c.cache[k]; ok {
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()

	// try direct providers
	v, err := c.resolveDirect(k)
	if err == nil {
		c.mu.Lock()
		// cache & record creation order the first time a key is produced
		if _, exists := c.cache[k]; !exists {
			c.cache[k] = v
			c.creationIndex[k] = c.createSeq
			c.createSeq++
		}
		c.mu.Unlock()
		return v, nil
	}

	// if err is not NotFoundError, return it
	if !isNotFound(err) {
		return nil, err
	}

	// try interface bindings
	if k.sliceElem == nil && k.typ.Kind() == reflect.Interface {
		v, err = c.resolveViaBind(k)
		if err == nil {
			c.mu.Lock()
			if _, exists := c.cache[k]; !exists {
				c.cache[k] = v
				c.creationIndex[k] = c.createSeq
				c.createSeq++
			}
			c.mu.Unlock()
			return v, nil
		}
		return nil, err
	}

	return nil, NotFoundError{Type: k.typ, Name: k.name}
}

func (c *Container) resolveDirect(k key) (any, error) {
	fns, ok := c.reg.providers[k]
	if !ok || len(fns) == 0 {
		return nil, NotFoundError{Type: k.typ, Name: k.name}
	}

	// sets: execute all providers and aggregate
	if k.sliceElem != nil {
		res := &resolver{c: c}
		slice := reflect.MakeSlice(k.typ, 0, len(fns))
		for _, fn := range fns {
			val, err := fn(res)
			if err != nil {
				return nil, err
			}
			elem := reflect.ValueOf(val)
			if !elem.IsValid() || !elem.Type().AssignableTo(k.sliceElem) {
				return nil, BadCastError{Key: k, From: elem.Type(), To: k.sliceElem}
			}
			slice = reflect.Append(slice, elem)
		}
		return slice.Interface(), nil
	}

	// non-set: last registered wins (allow overrides in tests)
	fn := fns[len(fns)-1]
	res := &resolver{c: c}
	val, err := fn(res)
	if err != nil {
		return nil, err
	}
	return val, nil
}

// resolveViaBind tries to find a concrete type for interface lookups.
func (c *Container) resolveViaBind(k key) (any, error) {
	for pk := range c.reg.binds {
		if pk.name == k.name {
			implType := c.reg.binds[pk]
			if implType == nil {
				return nil, fmt.Errorf("bind for %s is nil", k.typ)
			}
			if !implType.Implements(k.typ) {
				return nil, fmt.Errorf("bind for %s does not implement it: %s", k.typ, implType)
			}
			// try to resolve the concrete type
			return c.rawGet(key{typ: implType, name: pk.name})
		}
	}
	return nil, NotFoundError{Type: k.typ, Name: k.name}
}
