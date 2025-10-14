package assemble

import "reflect"

// RegOpt modifies registration behavior.
type RegOpt interface {
	apply(*registration)
}

type registration struct {
	name string
}

type nameOpt struct{ name string }

func (o nameOpt) apply(r *registration) {
	r.name = o.name
}

// Name specifies a name for the registration.
func Name(name string) RegOpt {
	return nameOpt{name: name}
}

// AsOpt encodes a "bind interface to implementation type" choice.
type AsOpt struct{ to reflect.Type }

// As specifies the interface type to bind to.
func As[T any]() AsOpt {
	var zero T
	return AsOpt{to: reflect.TypeOf(zero).Elem()}
}
