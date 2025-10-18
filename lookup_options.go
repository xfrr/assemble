package assemble

// KeyOpt modifies the key used to look up a dependency.
type KeyOpt interface {
	apply(*Key)
}

type withName struct{ name string }

func (o withName) apply(k *Key) {
	k.name = o.name
}

// WithName specifies a name for the dependency to look up.
func WithName(name string) KeyOpt {
	return withName{name: name}
}
