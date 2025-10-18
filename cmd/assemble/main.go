package main

import (
	"errors"
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
		varName    = flag.String("var", "Core", "assemble.Module variable name to parse (ignored if -fn is provided)")
		funcName   = flag.String("fn", "", "function name that returns assemble.Assemble(...) (optional)")
		outputPath = flag.String("o", "gen.go", "output file (e.g., ./build_assemble_gen.go)")
	)
	flag.Parse()
	if *outputPath == "" {
		log.Fatal("-o output file is required")
	}

	fset := token.NewFileSet()

	switch {
	case *funcName != "":
		mod, pkgPath, genFn, err := extractFromFunctionTwoPass(*pkgArg, *funcName, fset)
		if err != nil {
			log.Fatalf("parse function: %v", err)
		}
		writeOut(*outputPath, *varName, genFn, pkgPath, mod)

	default:
		// -------- Variable-export mode (single-pass) ----------
		cfg := loadConfig(dirFor(*pkgArg), nil, fset)
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

		mod.SetPkgName(p.Name)
		writeOut(*outputPath, *varName, "Assemble"+*varName, p.PkgPath, mod)
	}
}

func writeOut(outputPath, varName, genFnName, currentPkgPath string, mod parser.Model) {
	cfg := render.Config{
		IsVarBased:     varName != "",
		VarName:        varName,
		FuncName:       genFnName,
		PkgName:        mod.PkgName(),
		OutFilePath:    outputPath,
		CurrentPkgPath: currentPkgPath,
	}

	src, emitErr := render.EmitWithFunc(mod, cfg)
	if emitErr != nil {
		log.Fatalf("render: %v", emitErr)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(outputPath, []byte(src), 0o644); err != nil {
		log.Fatalf("write: %v", err)
	}
	fmt.Printf("assemblegen: wrote %s (%d bytes)\n", outputPath, len(src))
}

func extractFromFunctionTwoPass(pkgArg, funcName string, fset *token.FileSet) (parser.Model, string, string, error) {
	cfgA := loadConfigSyntaxOnly(dirFor(pkgArg), []string{"-tags=assemble_codegen"}, fset)
	pkgsA, err := packages.Load(cfgA, pkgArg)
	if err != nil {
		return parser.Model{}, "", "", fmt.Errorf("load (A): %w", err)
	}
	if len(pkgsA) == 0 {
		return parser.Model{}, "", "", fmt.Errorf("failed loading package (A) %q", pkgArg)
	}
	pA := pkgsA[0]

	ref, genFnName, refErr := parser.ExtractFunctionModuleRefLoose(pA, funcName, fset)
	if refErr != nil {
		return parser.Model{}, "", "", refErr
	}

	cfgB := loadConfig(dirFor(pkgArg), nil, fset)

	switch {
	case ref.IsLocalIdent:
		pkgsB, err := packages.Load(cfgB, pkgArg)
		if err != nil {
			return parser.Model{}, "", "", fmt.Errorf("load (B local): %w", err)
		}
		if packages.PrintErrors(pkgsB) > 0 || len(pkgsB) == 0 {
			return parser.Model{}, "", "", fmt.Errorf("failed loading package (B local) %q", pkgArg)
		}
		pB := pkgsB[0]
		mod, err := parser.ExtractModule(pB, ref.Ident, fset)
		if err != nil {
			return parser.Model{}, "", "", fmt.Errorf("extract local module %q: %w", ref.Ident, err)
		}
		// merge function-file imports so renderer can include them
		mod.Imports = append(mod.Imports, ref.Imports...)
		mod.SetPkgName(pB.Name)
		return mod, pB.PkgPath, genFnName, nil

	case ref.IsSelector:
		if ref.SelPkgPath == "" {
			return parser.Model{}, "", "", fmt.Errorf("could not resolve import path for alias %q", ref.SelPkgAlias)
		}
		pkgsB, err := packages.Load(cfgB, ref.SelPkgPath)
		if err != nil {
			return parser.Model{}, "", "", fmt.Errorf("load (B foreign %q): %w", ref.SelPkgPath, err)
		}
		if packages.PrintErrors(pkgsB) > 0 || len(pkgsB) == 0 {
			return parser.Model{}, "", "", fmt.Errorf("failed loading foreign package %q", ref.SelPkgPath)
		}
		q := pkgsB[0]
		mod, err := parser.ExtractModule(q, ref.SelIdent, fset)
		if err != nil {
			return parser.Model{}, "", "", fmt.Errorf("extract foreign module %s.%s: %w", ref.SelPkgPath, ref.SelIdent, err)
		}
		mod.Imports = append(mod.Imports, ref.Imports...)
		mod.SetPkgName(q.Name)
		return mod, q.PkgPath, genFnName, nil
	}

	return parser.Model{}, "", "", errors.New("unsupported module reference shape")
}

func loadConfig(dir string, buildFlags []string, fset *token.FileSet) *packages.Config {
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:   dir,
		Fset:  fset,
		Tests: false,
	}
	if len(buildFlags) > 0 {
		cfg.BuildFlags = buildFlags
	}
	return cfg
}

func loadConfigSyntaxOnly(dir string, buildFlags []string, fset *token.FileSet) *packages.Config {
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedSyntax,
		Dir:   dir,
		Fset:  fset,
		Tests: false,
	}
	if len(buildFlags) > 0 {
		cfg.BuildFlags = buildFlags
	}
	return cfg
}

func dirFor(arg string) string {
	if len(arg) >= 2 && arg[:2] == "./" {
		wd, _ := os.Getwd()
		return wd
	}
	return ""
}
