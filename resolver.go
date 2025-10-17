package assemble

import "context"

type resolver struct{ c *Container }

func (r *resolver) rawGet(ctx context.Context, k key) (any, error) { return r.c.rawGet(ctx, k) }
