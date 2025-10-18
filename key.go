package assemble

import (
	"reflect"
	"sync"
)

// typeCache stores reflect metadata per concrete T.
var typeCache sync.Map // map[reflect.Type]*typeMeta

// Key identifies a dependency by type (and optional name).
// If typ is a slice, sliceElem holds its element type to speed up set handling.
type Key struct {
	typ       reflect.Type
	name      string
	sliceElem reflect.Type
}

// KeyOf returns a precomputed key for T with no name.
// Use this in hot paths or generated code to avoid per-call option handling.
func KeyOf[T any]() Key {
	return makeKey(metaOf[T]())
}

// NamedKey returns a precomputed key for T with the given name.
// Name application is done once here, not on every Get call.
func NamedKey[T any](name string) Key {
	k := makeKey(metaOf[T]())
	k.name = name
	return k
}

/* =========================
   Backward-compatible API
   ========================= */

// keyFor is the legacy constructor that accepts options (may allocate when opts is non-empty).
// Prefer KeyOf / NamedKey in hot paths.
func keyFor[T any](opts ...KeyOpt) Key {
	k := makeKey(metaOf[T]())
	for _, o := range opts {
		o.apply(&k)
	}
	return k
}

/* =========================
   Internal: type metadata cache
   ========================= */

type typeMeta struct {
	t         reflect.Type
	sliceElem reflect.Type
}

// metaOf caches reflect info for T so we don't redo Kind/Elem tests on every call.
func metaOf[T any]() *typeMeta {
	t := typeOf[T]()

	if v, ok := typeCache.Load(t); ok {
		meta, metaOk := v.(*typeMeta)
		if metaOk {
			return meta
		}
	}

	// Compute and try to publish to the cache.
	m := &typeMeta{t: t}
	if t.Kind() == reflect.Slice {
		m.sliceElem = t.Elem()
	}

	// Handle concurrent writers: prefer the existing one if present.
	if old, loaded := typeCache.LoadOrStore(t, m); loaded {
		meta, metaOk := old.(*typeMeta)
		if metaOk {
			return meta
		}
	}
	return m
}

// makeKey constructs a Key from type metadata (no reflection work).
func makeKey(m *typeMeta) Key {
	return Key{typ: m.t, sliceElem: m.sliceElem}
}

// typeOf returns the reflect.Type for T without allocation.
func typeOf[T any]() reflect.Type {
	var z *T
	return reflect.TypeOf(z).Elem()
}
