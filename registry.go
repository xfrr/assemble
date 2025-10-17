package assemble

import (
	"context"
	"reflect"
)

// internal key for registry/cache
type key struct {
	typ       reflect.Type
	name      string
	sliceElem reflect.Type // if typ is a slice, this is the element type
}

func typeOf[T any]() reflect.Type {
	var z *T
	return reflect.TypeOf(z).Elem()
}

func keyFor[T any](opts ...KeyOpt) key {
	t := typeOf[T]()
	k := key{typ: t}
	for _, o := range opts {
		o.apply(&k)
	}
	if t.Kind() == reflect.Slice {
		k.sliceElem = t.Elem()
	}
	return k
}

type provideReg[T any] struct {
	p    Provider[T]
	opts []RegOpt
}

func (r *provideReg[T]) register(m *module) {
	meta := registration{}
	for _, o := range r.opts {
		o.apply(&meta)
	}
	m.addProvider(typeOf[T](), meta.name, func(ctx context.Context, res Resolver) (any, error) {
		return r.p(ctx, res)
	})
}

type setReg[T any] struct {
	providers []Provider[T]
	name      string
}

type Appender[T any] struct {
	p    Provider[T]
	opts []RegOpt
}

func (a Appender[T]) apply(s *setReg[T]) {
	if a.p != nil {
		s.providers = append(s.providers, a.p)
	}
	for _, o := range a.opts {
		o.apply(&registration{name: s.name})
	}
}

func (r *setReg[T]) register(m *module) {
	for _, p := range r.providers {
		m.addSetProvider(typeOf[T](), "", func(ctx context.Context, res Resolver) (any, error) {
			return p(ctx, res)
		})
	}
}

type bindReg struct {
	as   AsOpt
	opts []RegOpt
}

func (r *bindReg) register(m *module) {
	meta := registration{}
	for _, o := range r.opts {
		o.apply(&meta)
	}
	m.addBind(meta.name, r.as.to)
}
