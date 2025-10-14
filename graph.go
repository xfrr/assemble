package assemble

import (
	"fmt"
	"sort"
	"strings"
)

// ExportCreationOrderDOT returns a DOT graph (Graphviz) of the creation order.
// Rank is left-to-right; edges connect creation sequence.
func (c *Container) ExportCreationOrderDOT() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	type node struct {
		k key
		i int
	}
	nodes := make([]node, 0, len(c.creationIndex))
	for k, idx := range c.creationIndex {
		nodes = append(nodes, node{k: k, i: idx})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].i < nodes[j].i })
	var b strings.Builder
	b.WriteString("digraph CreationOrder {\n  rankdir=LR;\n  node [shape=box];\n")
	for _, n := range nodes {
		label := fmt.Sprintf("%d: %s", n.i, keyLabel(n.k))
		b.WriteString(fmt.Sprintf("  n%d [label=\"%s\"];\n", n.i, escape(label)))
	}
	for i := 0; i+1 < len(nodes); i++ {
		b.WriteString(fmt.Sprintf("  n%d -> n%d;\n", nodes[i].i, nodes[i+1].i))
	}
	b.WriteString("}\n")
	return b.String()
}

// ExportCreationOrderPlantUML returns a simple PlantUML activity diagram of creation order.
func (c *Container) ExportCreationOrderPlantUML() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	type node struct {
		k key
		i int
	}
	nodes := make([]node, 0, len(c.creationIndex))
	for k, idx := range c.creationIndex {
		nodes = append(nodes, node{k: k, i: idx})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].i < nodes[j].i })
	var b strings.Builder
	b.WriteString("@startuml\nstart\n")
	for _, n := range nodes {
		label := fmt.Sprintf("%d: %s", n.i, keyLabel(n.k))
		b.WriteString(fmt.Sprintf(": %s ;\n", escape(label)))
	}
	b.WriteString("stop\n@enduml\n")
	return b.String()
}

func keyLabel(k key) string {
	name := ""
	if k.name != "" {
		name = " name=" + k.name
	}
	if k.typ != nil {
		return k.typ.String() + name
	}
	return "interface" + name
}

func escape(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
