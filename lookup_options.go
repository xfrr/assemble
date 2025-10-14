package assemble

// KeyOpt modifies the key used to look up a dependency.
type KeyOpt interface {
	apply(*key)
}

type withName struct{ name string }

func (o withName) apply(k *key) {
	k.name = o.name
}

// WithName specifies a name for the dependency to look up.
func WithName(name string) KeyOpt {
	return withName{name: name}
}
