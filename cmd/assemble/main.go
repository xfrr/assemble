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

/* =========================
   CLI
   ========================= */

type cliArgs struct {
	pkgArg     string
	varName    string
	funcName   string
	outputPath string
}

func main() {
	log.SetFlags(0) // clean logs

	args := parseArgs()
	if err := run(args); err != nil {
		log.Fatal(err)
	}
}

func parseArgs() cliArgs {
	var a cliArgs
	flag.StringVar(&a.pkgArg, "pkg", ".", "Go package (import path or ./relative)")
	flag.StringVar(&a.varName, "var", "Core", "assemble.Module variable name to parse (ignored if -fn is provided)")
	flag.StringVar(&a.funcName, "fn", "", "function name that returns assemble.Assemble(...) (optional)")
	flag.StringVar(&a.outputPath, "o", "gen.go", "output file (e.g., ./build_assemble_gen.go)")
	flag.Parse()

	if a.outputPath == "" {
		log.Fatal("-o output file is required")
	}
	return a
}

func run(a cliArgs) error {
	fset := token.NewFileSet()

	switch {
	case a.funcName != "":
		// Two-pass: (A) loose AST to find the module reference, (B) typed load to parse it.
		mod, pkgPath, genFn, err := extractFromFunctionTwoPass(a.pkgArg, a.funcName, fset)
		if err != nil {
			return fmt.Errorf("parse function: %w", err)
		}
		return writeOut(a.outputPath, a.varName, genFn, pkgPath, mod)

	default:
		// Single-pass variable mode.
		cfg := loadConfig(dirFor(a.pkgArg), nil, fset)
		p, err := loadOnePackage(cfg, a.pkgArg, "load package")
		if err != nil {
			return err
		}

		mod, err := parser.ExtractModule(p, a.varName, fset)
		if err != nil {
			return fmt.Errorf("parse module: %w", err)
		}
		mod.SetPkgName(p.Name)

		return writeOut(a.outputPath, a.varName, "Assemble"+a.varName, p.PkgPath, mod)
	}
}

/* =========================
   Orchestration
   ========================= */

func writeOut(outputPath, varName, genFnName, currentPkgPath string, mod parser.Model) error {
	cfg := render.Config{
		IsVarBased:     varName != "",
		VarName:        varName,
		FuncName:       genFnName,
		PkgName:        mod.PkgName(),
		OutFilePath:    outputPath,
		CurrentPkgPath: currentPkgPath,
	}

	src, err := render.EmitWithFunc(mod, cfg)
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}

	if mkdirErr := os.MkdirAll(safeDir(outputPath), 0o755); mkdirErr != nil {
		return fmt.Errorf("mkdir: %w", mkdirErr)
	}
	if writeErr := os.WriteFile(outputPath, []byte(src), 0o644); writeErr != nil {
		return fmt.Errorf("write: %w", writeErr)
	}

	fmt.Printf("assemblegen: wrote %s (%d bytes)\n", outputPath, len(src))
	return nil
}

func extractFromFunctionTwoPass(pkgArg, funcName string, fset *token.FileSet) (parser.Model, string, string, error) {
	// Pass A: syntax-only (with build tag) to avoid referencing generated code.
	cfgA := loadConfigSyntaxOnly(dirFor(pkgArg), []string{"-tags=assemble_codegen"}, fset)
	pA, err := loadOnePackage(cfgA, pkgArg, "load (A)")
	if err != nil {
		return parser.Model{}, "", "", err
	}

	ref, genFnName, refErr := parser.ExtractFunctionModuleRefLoose(pA, funcName)
	if refErr != nil {
		return parser.Model{}, "", "", refErr
	}

	// Pass B: full typed load.
	cfgB := loadConfig(dirFor(pkgArg), nil, fset)

	switch {
	case ref.IsLocalIdent:
		pB, pBErr := loadOnePackage(cfgB, pkgArg, "load (B local)")
		if pBErr != nil {
			return parser.Model{}, "", "", pBErr
		}
		mod, modErr := parser.ExtractModule(pB, ref.Ident, fset)
		if modErr != nil {
			return parser.Model{}, "", "", fmt.Errorf("extract local module %q: %w", ref.Ident, modErr)
		}
		// Merge function-file imports so renderer can include them.
		mod.Imports = append(mod.Imports, ref.Imports...)
		mod.SetPkgName(pB.Name)
		return mod, pB.PkgPath, genFnName, nil

	case ref.IsSelector:
		if ref.SelPkgPath == "" {
			return parser.Model{}, "", "", fmt.Errorf("could not resolve import path for alias %q", ref.SelPkgAlias)
		}
		q, qErr := loadOnePackage(cfgB, ref.SelPkgPath, "load (B foreign)")
		if qErr != nil {
			return parser.Model{}, "", "", qErr
		}
		mod, modErr := parser.ExtractModule(q, ref.SelIdent, fset)
		if modErr != nil {
			return parser.Model{},
				"", "",
				fmt.Errorf("extract foreign module %s.%s: %w", ref.SelPkgPath, ref.SelIdent, modErr)
		}
		mod.Imports = append(mod.Imports, ref.Imports...)
		mod.SetPkgName(q.Name)
		return mod, q.PkgPath, genFnName, nil
	}

	return parser.Model{}, "", "", errors.New("unsupported module reference shape")
}

/* =========================
   packages helpers
   ========================= */

func loadOnePackage(cfg *packages.Config, pattern string, ctx string) (*packages.Package, error) {
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ctx, err)
	}
	if packages.PrintErrors(pkgs) > 0 || len(pkgs) == 0 {
		return nil, fmt.Errorf("failed %s %q", ctx, pattern)
	}
	return pkgs[0], nil
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

/* =========================
   small utils
   ========================= */

// dirFor returns the directory to set in packages.Config for relative patterns.
// Keeps original behavior but accepts "." and "./...".
func dirFor(arg string) string {
	if arg == "." || hasDotSlashPrefix(arg) {
		wd, _ := os.Getwd()
		return wd
	}
	return ""
}

func hasDotSlashPrefix(s string) bool {
	return len(s) >= 2 && s[:2] == "./"
}

func safeDir(p string) string {
	d := filepath.Dir(p)
	if d == "" || d == "." {
		return "."
	}
	return d
}
