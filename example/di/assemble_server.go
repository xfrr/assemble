//go:build assemble_codegen
// +build assemble_codegen

package di

import (
	"github.com/xfrr/assemble"
)

func AssembleServer() (*assemble.Container, error) {
	return assemble.Assemble(
		Core,
	)
}
