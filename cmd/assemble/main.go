package main

import (
	"flag"
	"fmt"
	"go/token"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/tools/go/packages"

	"github.com/xfrr/assemble/internal/parser"
	"github.com/xfrr/assemble/internal/render"
)

func main() {
	var (
		pkgArg  = flag.String("pkg", ".", "Go package (import path or ./relative)")
		varName = flag.String("var", "Core", "assemble.Module variable name to parse")
		out     = flag.String("o", "gen.go", "output file (e.g., ./build_assemble_gen.go)")
	)
	flag.Parse()
	if *out == "" {
		log.Fatal("-o output file is required")
	}

	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:   dirFor(*pkgArg),
		Fset:  fset,
		Tests: false,
	}

	pkgs, err := packages.Load(cfg, *pkgArg)
	if err != nil {
		log.Fatalf("load: %v", err)
	}
	if packages.PrintErrors(pkgs) > 0 || len(pkgs) == 0 {
		log.Fatalf("failed loading package %q", *pkgArg)
	}
	p := pkgs[0]

	mod, err := parser.ExtractModule(p, *varName, fset)
	if err != nil {
		log.Fatalf("parse module: %v", err)
	}

	src, err := render.Emit(*varName, p.Name, *out, mod)
	if err != nil {
		log.Fatalf("render: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(*out, []byte(src), 0o644); err != nil {
		log.Fatalf("write: %v", err)
	}

	fmt.Printf("assemblegen: wrote %s (%d bytes)\n", *out, len(src))
}

func dirFor(arg string) string {
	if len(arg) >= 2 && arg[:2] == "./" {
		wd, _ := os.Getwd()
		return wd
	}
	return ""
}
