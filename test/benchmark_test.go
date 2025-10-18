package benchgen_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xfrr/assemble"
	benchgen "github.com/xfrr/assemble/test"
)

type Node struct {
	Idx  int
	Prev *Node
}

type Comp struct {
	Layer int
	Col   int
	Prev  *Comp
}

// ---------------------------
// Named resolution helpers
// ---------------------------

// Replace this with your library's named get if different.
func getNode(r assemble.Resolver, name string) (*Node, error) {
	return assemble.GetByKey[*Node](context.Background(), r, assemble.NamedKey[*Node](name))
}

func getComp(r assemble.Resolver, name string) (*Comp, error) {
	return assemble.GetByKey[*Comp](context.Background(), r, assemble.NamedKey[*Comp](name))
}

// ---------------------------
// Module builders
// ---------------------------

func createDependencyChain(depth int) assemble.Module {
	var regs assemble.Module

	// Base node n0 (no dependency)
	regs = append(regs, assemble.Provide(func(_ context.Context, _ assemble.Resolver) (*Node, error) {
		return &Node{Idx: 0, Prev: nil}, nil
	}, assemble.Name("n0")))

	// Chain providers n(i) -> n(i-1)
	for i := 1; i < depth; i++ {
		nameCur := fmt.Sprintf("n%d", i)
		namePrev := fmt.Sprintf("n%d", i-1)

		iLocal := i
		regs = append(regs, assemble.Provide(func(_ context.Context, r assemble.Resolver) (*Node, error) {
			prev, err := getNode(r, namePrev)
			if err != nil {
				return nil, err
			}
			return &Node{Idx: iLocal, Prev: prev}, nil
		}, assemble.Name(nameCur)))
	}

	// Force full traversal by resolving the tail at start
	tail := fmt.Sprintf("n%d", depth-1)
	regs = append(regs, assemble.OnStart(func(_ context.Context, r assemble.Resolver) error {
		_, err := getNode(r, tail)
		return err
	}, assemble.WithStartTimeout(5*time.Second)))

	return regs
}

// Depth × Breadth benchmark:
// - Build (layers-1) layers of named singletons for each column chain.
// - Last layer is provided via a Set[*Comp] with 'breadth' elements, each depending on its column's previous layer.
// - OnStart resolves [] *Comp to measure set assembly + deep chains.
func buildLayeredSetModule(layers, breadth int) assemble.Module {
	var regs assemble.Module

	// Build all columns chains up to the last layer-1
	for col := range breadth {
		baseName := fmt.Sprintf("c%d-l0", col)
		colLocal := col

		// Base element for each column
		regs = append(regs, assemble.Provide(func(_ context.Context, _ assemble.Resolver) (*Comp, error) {
			return &Comp{Layer: 0, Col: colLocal, Prev: nil}, nil
		}, assemble.Name(baseName)))

		// Intermediate layers 1..layers-2 (named singletons)
		for l := 1; l < layers-1; l++ {
			cur := fmt.Sprintf("c%d-l%d", col, l)
			prev := fmt.Sprintf("c%d-l%d", col, l-1)
			lLocal := l

			regs = append(regs, assemble.Provide(func(_ context.Context, r assemble.Resolver) (*Comp, error) {
				pv, err := getComp(r, prev)
				if err != nil {
					return nil, err
				}
				return &Comp{Layer: lLocal, Col: colLocal, Prev: pv}, nil
			}, assemble.Name(cur)))
		}
	}

	// Last layer as a Set[*Comp] with 'breadth' elements
	var appends []assemble.Appender[*Comp]
	for col := range breadth {
		prev := fmt.Sprintf("c%d-l%d", col, layers-2)
		colLocal := col
		layerLocal := layers - 1

		appends = append(appends, assemble.Append(func(_ context.Context, r assemble.Resolver) (*Comp, error) {
			pv, err := getComp(r, prev)
			if err != nil {
				return nil, err
			}
			// Last element for this column
			return &Comp{Layer: layerLocal, Col: colLocal, Prev: pv}, nil
		}))
	}
	regs = append(regs, assemble.Set(appends...))

	// OnStart: resolve the last-layer Set to force assembling breadth chains
	regs = append(regs, assemble.OnStart(func(ctx context.Context, r assemble.Resolver) error {
		_, err := assemble.GetByKey[[]*Comp](ctx, r, assemble.NamedKey[[]*Comp](""))
		return err
	}, assemble.WithStartTimeout(5*time.Second)))

	return regs
}

// ---------------------------
// Benchmarks
// ---------------------------

func BenchmarkCompiled_BuildStartStop_VaryDepth(b *testing.B) {
	for _, depth := range []int{10} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			b.SetBytes(int64(depth)) // per-iteration "bytes" represent nodes constructed
			benchBuildStartStop(b, benchgen.AssembleNodeDependencyGraph)
		})
	}
}

// Vary Depth: 100, 1,000, 5,000 — confirm linear scaling.
func BenchmarkInterpreted_BuildStartStop_VaryDepth(b *testing.B) {
	for _, depth := range []int{10, 100, 1_000, 5_000} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			mod := createDependencyChain(depth)
			b.SetBytes(int64(depth)) // per-iteration "bytes" represent nodes constructed
			benchBuildStartStop(b, func() (*assemble.Container, error) {
				return assemble.Assemble(mod)
			})
		})
	}
}

func BenchmarkInterpreted_Resolve_DeepTail_VaryDepth(b *testing.B) {
	for _, depth := range []int{10, 100, 1_000, 5_000} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			mod := createDependencyChain(depth)

			c, err := assemble.Assemble(mod)
			if err != nil {
				b.Fatalf("assemble: %v", err)
			}
			if startErr := c.Start(context.Background()); startErr != nil {
				b.Fatalf("start: %v", startErr)
			}
			defer c.Shutdown(context.Background())

			benchResolveTail(b, c, fmt.Sprintf("n%d", depth-1))
		})
	}
}

// 2) 100 layers × 50 breadth — uses Set assembly at the last layer.
func BenchmarkInterpreted_BuildStartStop_Layers100_Breadth50(b *testing.B) {
	const (
		layers  = 100
		breadth = 50
	)
	mod := buildLayeredSetModule(layers, breadth)
	// Each iteration builds layers*breadth nodes (last layer assembled via Set)
	b.SetBytes(int64(layers * breadth))
	benchBuildStartStop(b, func() (*assemble.Container, error) {
		return assemble.Assemble(mod)
	})
}

func BenchmarkInterpreted_Resolve_SetLastLayer_Layers100_Breadth50(b *testing.B) {
	const (
		layers  = 100
		breadth = 50
	)
	mod := buildLayeredSetModule(layers, breadth)

	c, err := assemble.Assemble(mod)
	if err != nil {
		b.Fatalf("assemble: %v", err)
	}
	if startErr := c.Start(context.Background()); startErr != nil {
		b.Fatalf("start: %v", startErr)
	}
	defer c.Shutdown(context.Background())

	// Resolve the set repeatedly — measures set read/aggregation + cache fast path
	b.ReportAllocs()

	r := assemble.Resolver(c)
	for b.Loop() {
		if _, getErr := assemble.Get[[]*Comp](context.Background(), r); getErr != nil {
			b.Fatalf("get set last-layer: %v", getErr)
		}
	}
}

// ---------------------------
// Helpers
// ---------------------------

func benchBuildStartStop(b *testing.B, build func() (*assemble.Container, error)) {
	b.ReportAllocs()
	ctx := context.Background()

	for b.Loop() {
		c, err := build()
		if err != nil {
			b.Fatalf("build: %v", err)
		}
		if startErr := c.Start(ctx); startErr != nil {
			b.Fatalf("start: %v", startErr)
		}
		if shutdownErr := c.Shutdown(ctx); shutdownErr != nil {
			b.Fatalf("shutdown: %v", shutdownErr)
		}
	}
}

// Resolve the deep-chain tail repeatedly (keeps pressure on the deepest path).
func benchResolveTail(b *testing.B, c *assemble.Container, tailName string) {
	b.ReportAllocs()

	r := assemble.Resolver(c)
	for b.Loop() {
		if _, getErr := getNode(r, tailName); getErr != nil {
			b.Fatalf("get tail %q: %v", tailName, getErr)
		}
	}
}
