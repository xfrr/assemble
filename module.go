package assemble

import (
	"context"
	"reflect"
)

// Module groups related registrations.
type Module []Registrar

func (m Module) clone() Module {
	cp := make(Module, len(m))
	copy(cp, m)
	return cp
}

type module struct {
	// map[key] -> list of providers. If multiple, last wins for non-set lookups.
	providers map[Key][]func(context.Context, Resolver) (any, error)
	// interface bindings: iface key -> impl type (concrete)
	binds map[Key]reflect.Type
	// starts to run on Start (Invoke/OnStart)
	starts []startHook
	// stop hooks to run on Stop
	stops []stopHook
}

func newModule() *module {
	return &module{
		providers: make(map[Key][]func(context.Context, Resolver) (any, error)),
		binds:     make(map[Key]reflect.Type),
	}
}

func (m *module) addProvider(t reflect.Type, name string, fn func(context.Context, Resolver) (any, error)) {
	k := Key{typ: t, name: name}
	m.providers[k] = append(m.providers[k], fn)
}

func (m *module) addSetProvider(elem reflect.Type, name string, fn func(context.Context, Resolver) (any, error)) {
	slice := reflect.SliceOf(elem)
	k := Key{typ: slice, name: name, sliceElem: elem}
	m.providers[k] = append(m.providers[k], fn)
}

func (m *module) addBind(name string, to reflect.Type) {
	k := Key{typ: nil, name: name}
	m.binds[k] = to
}

func (m *module) addStop(h stopHook)   { m.stops = append(m.stops, h) }
func (m *module) addStart(h startHook) { m.starts = append(m.starts, h) }
