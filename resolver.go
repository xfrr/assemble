package assemble

type resolver struct{ c *Container }

func (r *resolver) rawGet(k key) (any, error) { return r.c.rawGet(k) }
