package benchgen

import (
	"context"

	"github.com/xfrr/assemble"
)

//go:generate assemble -pkg ./benchmark_assemble.go -var NodeDependencyGraph -o ./benchmark_assemble_gen.go
var NodeDependencyGraph = assemble.Module{
	assemble.Provide(func(_ context.Context, _ assemble.Resolver) (*Node, error) {
		return &Node{Idx: 1}, nil
	}),
	assemble.Provide(func(_ context.Context, r assemble.Resolver) (*Node, error) {
		prev, err := getNode(r, "node1")
		if err != nil {
			return nil, err
		}
		return &Node{Idx: 2, Prev: prev}, nil
	}),
	assemble.Provide(func(_ context.Context, r assemble.Resolver) (*Node, error) {
		prev, err := getNode(r, "node2")
		if err != nil {
			return nil, err
		}
		return &Node{Idx: 3, Prev: prev}, nil
	}),
	assemble.Provide(func(_ context.Context, _ assemble.Resolver) (*Node, error) {
		return &Node{Idx: 4}, nil
	}),
	assemble.Provide(func(_ context.Context, r assemble.Resolver) (*Node, error) {
		prev, err := getNode(r, "node4")
		if err != nil {
			return nil, err
		}
		return &Node{Idx: 5, Prev: prev}, nil
	}),
	assemble.Provide(func(_ context.Context, r assemble.Resolver) (*Node, error) {
		prev, err := getNode(r, "node5")
		if err != nil {
			return nil, err
		}
		return &Node{Idx: 6, Prev: prev}, nil
	}),
	assemble.Provide(func(_ context.Context, r assemble.Resolver) (*Node, error) {
		prev, err := getNode(r, "node3")
		if err != nil {
			return nil, err
		}
		return &Node{Idx: 7, Prev: prev}, nil
	}),
	assemble.Provide(func(_ context.Context, r assemble.Resolver) (*Node, error) {
		prev, err := getNode(r, "node2")
		if err != nil {
			return nil, err
		}
		return &Node{Idx: 8, Prev: prev}, nil
	}),
	assemble.Provide(func(_ context.Context, r assemble.Resolver) (*Node, error) {
		prev, err := getNode(r, "node6")
		if err != nil {
			return nil, err
		}
		return &Node{Idx: 9, Prev: prev}, nil
	}),
	assemble.Provide(func(_ context.Context, r assemble.Resolver) (*Node, error) {
		prev, err := getNode(r, "node9")
		if err != nil {
			return nil, err
		}
		return &Node{Idx: 10, Prev: prev}, nil
	}),
}

type Node struct {
	Idx  int
	Prev *Node
}

// Replace this with your library's named get if different.
func getNode(r assemble.Resolver, name string) (*Node, error) {
	return assemble.GetByKey[*Node](context.Background(), r, assemble.NamedKey[*Node](name))
}
