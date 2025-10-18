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
		pkgArg     = flag.String("pkg", ".", "Go package (import path or ./relative)")
		varName    = flag.String("var", "Core", "assemble.Module variable name to parse")
		outputPath = flag.String("o", "gen.go", "output file (e.g., ./build_assemble_gen.go)")
	)
	flag.Parse()
	if *outputPath == "" {
		log.Fatal("-o output file is required")
	}

	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:   dirFor(*pkgArg),
		Fset:  fset,
		Tests: false,
	}

	pkgs, loadErr := packages.Load(cfg, *pkgArg)
	if loadErr != nil {
		log.Fatalf("load: %v", loadErr)
	}
	if packages.PrintErrors(pkgs) > 0 || len(pkgs) == 0 {
		log.Fatalf("failed loading package %q", *pkgArg)
	}
	p := pkgs[0]

	mod, extractErr := parser.ExtractModule(p, *varName, fset)
	if extractErr != nil {
		log.Fatalf("parse module: %v", extractErr)
	}

	src, emitErr := render.Emit(*varName, p.Name, *outputPath, mod)
	if emitErr != nil {
		log.Fatalf("render: %v", emitErr)
	}

	if mkdirErr := os.MkdirAll(filepath.Dir(*outputPath), 0o755); mkdirErr != nil {
		log.Fatalf("mkdir: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(*outputPath, []byte(src), 0o644); writeErr != nil {
		log.Fatalf("write: %v", writeErr)
	}

	fmt.Printf("assemblegen: wrote %s (%d bytes)\n", *outputPath, len(src))
}

func dirFor(arg string) string {
	if len(arg) >= 2 && arg[:2] == "./" {
		wd, _ := os.Getwd()
		return wd
	}
	return ""
}
