package assemble

import (
	"reflect"
	"sync"
)

type Key struct {
	typ       reflect.Type
	name      string
	sliceElem reflect.Type // if typ is a slice, this is the element type
}

// KeyOf returns a precomputed key for T with no name.
// Use this in hot paths or generated code to avoid per-call key construction.
func KeyOf[T any]() Key {
	return keyFor[T]()
}

// NamedKey returns a precomputed key for T with the given name.
// Name application is done once here, not on every Get.
func NamedKey[T any](name string) Key {
	k := keyFor[T]()
	k.name = name
	return k
}

// keyFor is the legacy constructor that accepts options (causes alloc at callsite).
// Prefer keyForType / namedKeyForType in hot paths.
func keyFor[T any](opts ...KeyOpt) Key {
	// this allocates when opts is non-empty due to variadic slice creation.
	meta := metaOf[T]()
	k := Key{typ: meta.t, sliceElem: meta.sliceElem}
	for _, o := range opts {
		o.apply(&k)
	}
	return k
}

type typeMeta struct {
	t         reflect.Type
	sliceElem reflect.Type
}

var typeCache sync.Map // map[reflect.Type]*typeMeta

// metaOf caches reflect info for T so we don't redo Kind/Elem tests on every call.
func metaOf[T any]() *typeMeta {
	t := typeOf[T]()
	if v, ok := typeCache.Load(t); ok {
		vType, _ := v.(*typeMeta)
		return vType
	}

	m := &typeMeta{t: t}
	if t.Kind() == reflect.Slice {
		m.sliceElem = t.Elem()
	}

	if old, loaded := typeCache.LoadOrStore(t, m); loaded {
		oldType, _ := old.(*typeMeta)
		return oldType
	}
	return m
}

// typeOf returns the reflect.Type for T without allocation.
func typeOf[T any]() reflect.Type {
	var z *T
	return reflect.TypeOf(z).Elem()
}
