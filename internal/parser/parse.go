package parser

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"path"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

const (
	pkgAssemble     = "assemble"
	typeModuleIdent = "Module"          // identifier when unqualified (same package)
	typeModuleQual  = "assemble.Module" // qualified type hint in errors

	fnProvide    = "Provide"
	fnSet        = "Set"
	fnBind       = "Bind"
	fnAppend     = "Append"
	fnInvoke     = "Invoke"
	fnOnStart    = "OnStart"
	fnOnStartFor = "OnStartFor"
	fnOnStop     = "OnStop"
	fnOnStopFor  = "OnStopFor"

	fnName = "Name"
	fnAs   = "As"

	fnWithPriority     = "WithPriority"
	fnWithStartTimeout = "WithStartTimeout"
	fnWithStopTimeout  = "WithStopTimeout"

	kindStart = "start"
	kindStop  = "stop"
)

// ExtractModule scans the given package for a variable named `varName`
// and attempts to parse its value as an assemble module definition.
func ExtractModule(p *packages.Package, varName string, fset *token.FileSet) (Model, error) {
	var mdl Model
	var found bool
	var firstErr error

	for _, file := range p.Syntax {
		ok, m, err := extractFromFile(p, file, varName, fset)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if ok {
			mdl = m
			found = true
			break
		}
	}

	if firstErr != nil {
		return mdl, fmt.Errorf("parsing module %q: %w", varName, firstErr)
	}
	if !found {
		return mdl, fmt.Errorf("module %q not found", varName)
	}
	return mdl, nil
}

/* =========================
   File-level extraction
   ========================= */

// extractFromFile scans one file, and if it finds `varName`, parses its module value
// (composite literal, call-based Module({...}), or selector to external package).
func extractFromFile(p *packages.Package, f *ast.File, varName string, fset *token.FileSet) (bool, Model, error) {
	var mdl Model
	var perFileErr error
	found := false

	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}

		for i, name := range vs.Names {
			if name.Name != varName || i >= len(vs.Values) {
				continue
			}

			switch v := vs.Values[i].(type) {
			case *ast.CompositeLit:
				// Core case: assemble.Module{ ... }
				m, err := parseModuleComposite(p, v, fset)
				if err != nil {
					perFileErr = err
					return false
				}
				m.Imports = collectFileImports(p, f)
				mdl, found = m, true
				return false

			case *ast.CallExpr:
				// Handle: assemble.Module({...}) or Module({...})
				if parseOk, m, err := parseModuleFromCall(p, v, fset); parseOk {
					if err != nil {
						perFileErr = err
						return false
					}
					m.Imports = collectFileImports(p, f)
					mdl, found = m, true
					return false
				}

			case *ast.SelectorExpr:
				// Handle: otherpkg.Core
				m, err := parseModuleFromSelector(p, v, fset)
				if err != nil {
					perFileErr = err
					return false
				}
				// Merge imports from all files of the imported package.
				impPkgPath := pkgPathOf(p, v)
				if impPkg := p.Imports[impPkgPath]; impPkg != nil {
					for _, impFile := range impPkg.Syntax {
						m.Imports = append(m.Imports, collectFileImports(impPkg, impFile)...)
					}
				}
				mdl, found = m, true
				return false
			}
		}
		return true
	})

	return found, mdl, perFileErr
}

/* =========================
   Parse module forms
   ========================= */

func parseModuleComposite(p *packages.Package, cl *ast.CompositeLit, fset *token.FileSet) (Model, error) {
	var out Model
	if !isAllowedType(cl.Type) {
		return out, fmt.Errorf("module must be of type %s (got %s)", typeModuleQual, exprText(cl.Type))
	}
	for _, elt := range cl.Elts {
		ce, ok := elt.(*ast.CallExpr)
		if !ok {
			continue
		}
		parseTopCall(p, ce, &out, fset)
	}
	return out, nil
}

// parseModuleFromCall handles constructs like:
//
//	assemble.Module({...})
//	Module({...})
func parseModuleFromCall(p *packages.Package, ce *ast.CallExpr, fset *token.FileSet) (bool, Model, error) {
	var out Model

	fn, _ := funName(p, ce.Fun)
	if fn != typeModuleIdent {
		return false, out, nil
	}
	if len(ce.Args) != 1 {
		return true, out, fmt.Errorf("expected %s({...}) with one argument", typeModuleIdent)
	}
	cl, ok := ce.Args[0].(*ast.CompositeLit)
	if !ok {
		return true, out, fmt.Errorf("%s(...) must receive a composite literal", typeModuleIdent)
	}
	m, err := parseModuleComposite(p, cl, fset)
	return true, m, err
}

// parseModuleFromSelector handles `otherpkg.Core` pointing to another package's variable.
func parseModuleFromSelector(p *packages.Package, se *ast.SelectorExpr, fset *token.FileSet) (Model, error) {
	var out Model

	pkgPath := pkgPathOf(p, se)
	pkgAlias, _ := funName(p, se.X)

	impPkg := p.Imports[pkgPath]
	if impPkg == nil {
		return out, fmt.Errorf("imported package %q not found", pkgPath)
	}
	if impPkg.Name != pkgAlias {
		// If aliased, try to match imported package by alias+path pair.
		var aliased *packages.Package
		for _, ip := range p.Imports {
			if ip.Name == pkgAlias && ip.PkgPath == pkgPath {
				aliased = ip
				break
			}
		}
		if aliased == nil {
			return out, fmt.Errorf("imported package %q with alias %q not found", pkgPath, pkgAlias)
		}
		impPkg = aliased
	}

	// Recurse into the imported package to extract the module there.
	m, err := ExtractModule(impPkg, se.Sel.Name, fset)
	if err != nil {
		return out, fmt.Errorf("extracting module from %q: %w", path.Join(pkgPath, se.Sel.Name), err)
	}
	return m, nil
}

/* =========================
   Registry call parsing
   ========================= */

func parseTopCall(p *packages.Package, ce *ast.CallExpr, out *Model, fset *token.FileSet) {
	fnName, _ := funName(p, ce.Fun)
	switch fnName {
	case fnProvide:
		if pr, ok := parseProvide(p, ce, fset); ok {
			out.Provides = append(out.Provides, pr)
		}
	case fnSet:
		if st, ok := parseSet(p, ce); ok {
			out.Sets = append(out.Sets, st)
		}
	case fnBind:
		if b, ok := parseBind(p, ce); ok {
			out.Binds = append(out.Binds, b)
		}
	case fnInvoke, fnOnStart:
		out.Starts = append(out.Starts, parseStartLike(p, ce, fset))
	case fnOnStartFor:
		out.Starts = append(out.Starts, parseStartFor(p, ce, fset))
	case fnOnStop:
		out.Stops = append(out.Stops, parseStopLike(p, ce, fset))
	case fnOnStopFor:
		out.Stops = append(out.Stops, parseStopFor(p, ce, fset))
	}
}

func parseProvide(p *packages.Package, ce *ast.CallExpr, fset *token.FileSet) (Provide, bool) {
	var pr Provide
	if len(ce.Args) == 0 {
		return pr, false
	}

	switch a := ce.Args[0].(type) {
	case *ast.FuncLit:
		pr.IsFuncLit = true
		pr.RawFuncLit = printNode(fset, a)

		// Count params/results from AST (works even if types.Info is missing).
		if a.Type != nil && a.Type.Params != nil {
			pr.LitParamCount = len(a.Type.Params.List)
		}
		if a.Type != nil && a.Type.Results != nil {
			pr.LitResultCount = len(a.Type.Results.List)
		}

		// Prefer type info if available.
		if typ, ok := p.TypesInfo.Types[a]; ok && typ.Type != nil {
			if sig, sigOk := typ.Type.(*types.Signature); sigOk {
				fillProvideFromSignature(p, &pr, sig)
			}
		}

		// Heuristics if type info wasn't conclusive.
		if pr.ResType == "" && a.Type != nil && a.Type.Results != nil {
			switch pr.LitResultCount {
			case 1:
				pr.ResType = exprText(a.Type.Results.List[0].Type)
				pr.HasError = false
			case 2:
				pr.ResType = exprText(a.Type.Results.List[0].Type)
				pr.HasError = true
			}
		}

		// Detect (ctx, Resolver) arity by AST text if types not available.
		if !pr.TakesResolve && pr.LitParamCount >= 1 && a.Type != nil && a.Type.Params != nil {
			tyTxt := exprText(a.Type.Params.List[0].Type)
			if strings.HasSuffix(tyTxt, ".Resolver") || strings.HasSuffix(tyTxt, ".Resolve") || tyTxt == "Resolver" || tyTxt == "Resolve" {
				pr.TakesResolve = true
			}
		}

	default:
		// named/selector ctor
		fillProvideFromExpr(p, &pr, a)
	}

	// Options: Name("…")
	pr.Named = extractNameOption(p, ce.Args[1:])
	return pr, true
}

func parseSet(p *packages.Package, ce *ast.CallExpr) (Set, bool) {
	var st Set
	st.ElemType = indexTypeArgText(ce.Fun)

	for _, a := range ce.Args {
		call, ok := a.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			continue
		}
		fn, _ := funName(p, call.Fun)
		if fn != fnAppend {
			continue
		}
		var pr Provide
		fillProvideFromExpr(p, &pr, call.Args[0])
		st.Elems = append(st.Elems, pr)
	}
	return st, true
}

func parseBind(p *packages.Package, ce *ast.CallExpr) (Bind, bool) {
	var b Bind
	// Bind[I](As[*Impl](), [Name("…")])
	b.IfaceType = indexTypeArgText(ce.Fun)
	if len(ce.Args) == 0 {
		return b, false
	}

	if asCall, ok := ce.Args[0].(*ast.CallExpr); ok {
		switch fun := asCall.Fun.(type) {
		case *ast.IndexExpr:
			if id, idOk := fun.X.(*ast.Ident); idOk && id.Name == fnAs {
				b.ImplType = exprText(fun.Index)
			}
		case *ast.IndexListExpr:
			if id, idOk := fun.X.(*ast.Ident); idOk && id.Name == fnAs && len(fun.Indices) == 1 {
				b.ImplType = exprText(fun.Indices[0])
			}
		}
	}
	b.Named = extractNameOption(p, ce.Args[1:])
	if b.IfaceType == "" || b.ImplType == "" {
		return b, false
	}
	return b, true
}

/* =========================
   Hooks
   ========================= */

func parseStartLike(p *packages.Package, ce *ast.CallExpr, fset *token.FileSet) StartHook {
	var sh StartHook
	if len(ce.Args) > 0 {
		isLit, raw, fn, alias, path := hookCallee(p, ce.Args[0], fset)
		sh.IsFuncLit, sh.RawFuncLit, sh.FuncName, sh.PkgAlias, sh.PkgPath = isLit, raw, fn, alias, path
	}
	sh.Priority, sh.TimeoutExpr, sh.TimeoutMs = parseHookOptions(p, ce.Args[1:], kindStart, true)
	return sh
}

func parseStopLike(p *packages.Package, ce *ast.CallExpr, fset *token.FileSet) StopHook {
	var eh StopHook
	if len(ce.Args) > 0 {
		isLit, raw, fn, alias, path := hookCallee(p, ce.Args[0], fset)
		eh.IsFuncLit, eh.RawFuncLit, eh.FuncName, eh.PkgAlias, eh.PkgPath = isLit, raw, fn, alias, path
	}
	eh.Priority, eh.TimeoutExpr, eh.TimeoutMs = parseHookOptions(p, ce.Args[1:], kindStop, true)
	return eh
}

func parseStartFor(p *packages.Package, ce *ast.CallExpr, fset *token.FileSet) StartHook {
	var sh StartHook
	sh.IsStartFor = true
	sh.ForType = indexTypeArgText(ce.Fun)

	if len(ce.Args) > 0 {
		isLit, raw, fn, alias, path := hookCallee(p, ce.Args[0], fset)
		sh.IsFuncLit, sh.RawFuncLit, sh.FuncName, sh.PkgAlias, sh.PkgPath = isLit, raw, fn, alias, path
		if !isLit {
			sh.FnHasCtx, sh.FnHasResolve = parseForSignatureFlags(p, ce.Args[0])
		}
	}
	// For-variants: keep only numeric timeout; avoid embedding complex exprs.
	prio, _, ms := parseHookOptions(p, ce.Args[1:], kindStart, false)
	sh.Priority, sh.TimeoutMs = prio, ms
	return sh
}

func parseStopFor(p *packages.Package, ce *ast.CallExpr, fset *token.FileSet) StopHook {
	var eh StopHook
	eh.IsStopFor = true
	eh.ForType = indexTypeArgText(ce.Fun)

	if len(ce.Args) > 0 {
		isLit, raw, fn, alias, path := hookCallee(p, ce.Args[0], fset)
		eh.IsFuncLit, eh.RawFuncLit, eh.FuncName, eh.PkgAlias, eh.PkgPath = isLit, raw, fn, alias, path
		if !isLit {
			eh.FnHasCtx, eh.FnHasResolve = parseForSignatureFlags(p, ce.Args[0])
		}
	}
	prio, _, ms := parseHookOptions(p, ce.Args[1:], kindStop, false)
	eh.Priority, eh.TimeoutMs = prio, ms
	return eh
}

/* =========================
   Imports (for renderer)
   ========================= */

// collectFileImports returns non-blank, non-dot imports from the file,
// resolving alias as: explicit alias → loaded package name → last path segment.
func collectFileImports(p *packages.Package, f *ast.File) []Import {
	seen := make(map[string]struct{})
	var out []Import

	for _, imp := range f.Imports {
		pathStr, _ := strconv.Unquote(imp.Path.Value)
		if pathStr == "" {
			continue
		}
		// Skip side-effect (_) or dot (.) imports to avoid ambiguous rendering.
		if imp.Name != nil && (imp.Name.Name == "_" || imp.Name.Name == ".") {
			continue
		}

		var alias string
		if imp.Name != nil {
			alias = imp.Name.Name
		} else if pkg := p.Imports[pathStr]; pkg != nil && pkg.Name != "" {
			alias = pkg.Name
		} else {
			alias = path.Base(pathStr)
		}

		if _, dup := seen[pathStr]; dup {
			continue
		}
		seen[pathStr] = struct{}{}
		out = append(out, Import{Alias: alias, Path: pathStr})
	}
	return out
}

/* =========================
   Provide signature helpers
   ========================= */

func fillProvideFromExpr(p *packages.Package, pr *Provide, expr ast.Expr) {
	pr.FuncName, pr.PkgAlias = funName(p, expr)
	pr.PkgPath = pkgPathOf(p, expr)
	if obj := objectOf(p, expr); obj != nil {
		if sig, ok := obj.Type().(*types.Signature); ok {
			fillProvideFromSignature(p, pr, sig)
		}
	}
}

func fillProvideFromSignature(p *packages.Package, pr *Provide, sig *types.Signature) {
	if sig.Params().Len() == 1 && isResolverLike(sig.Params().At(0).Type()) {
		pr.TakesResolve = true
	}
	switch sig.Results().Len() {
	case 1:
		pr.ResType = typeString(p, sig.Results().At(0).Type())
		pr.HasError = false
	case 2:
		pr.ResType = typeString(p, sig.Results().At(0).Type())
		pr.HasError = isErrorType(sig.Results().At(1).Type())
	}
}

func extractNameOption(p *packages.Package, args []ast.Expr) string {
	for _, arg := range args {
		call, ok := arg.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			continue
		}
		nm, _ := funName(p, call.Fun)
		if nm != fnName {
			continue
		}
		if lit, litOk := call.Args[0].(*ast.BasicLit); litOk && lit.Kind == token.STRING {
			v, _ := strconv.Unquote(lit.Value)
			return v
		}
	}
	return ""
}

/* =========================
   Hook helpers
   ========================= */

func hookCallee(p *packages.Package, arg ast.Expr, fset *token.FileSet) (isFuncLit bool, raw, fn, alias, pkgPath string) {
	switch a := arg.(type) {
	case *ast.FuncLit:
		return true, printNode(fset, a), "", "", ""
	default:
		fn, alias = funName(p, a)
		pkgPath = pkgPathOf(p, a)
		return false, "", fn, alias, pkgPath
	}
}

// parseHookOptions parses priority and timeout options for start/stop hooks.
// If captureExpr is true, TimeoutExpr preserves the original duration expression.
func parseHookOptions(p *packages.Package, args []ast.Expr, kind string, captureExpr bool) (priority int, timeoutExpr string, timeoutMs int64) {
	timeoutFn := fnWithStartTimeout
	if kind == kindStop {
		timeoutFn = fnWithStopTimeout
	}
	for _, arg := range args {
		call, ok := arg.(*ast.CallExpr)
		if !ok {
			continue
		}
		nm, _ := funName(p, call.Fun)
		switch nm {
		case fnWithPriority:
			if v, vOk := parsePriorityCall(call); vOk {
				priority = v
			}
		case timeoutFn:
			if captureExpr && len(call.Args) == 1 {
				timeoutExpr = exprText(call.Args[0])
			}
			timeoutMs = extractDurationMillis(p, call)
		}
	}
	return priority, timeoutExpr, timeoutMs
}

func parsePriorityCall(call *ast.CallExpr) (int, bool) {
	if len(call.Args) != 1 {
		return 0, false
	}
	bl, ok := call.Args[0].(*ast.BasicLit)
	if !ok || bl.Kind != token.INT {
		return 0, false
	}
	v, err := strconv.Atoi(bl.Value)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseForSignatureFlags(p *packages.Package, expr ast.Expr) (bool, bool) {
	var hasCtx, hasResolve bool
	obj := objectOf(p, expr)
	if obj == nil {
		return false, false
	}
	if sig, ok := obj.Type().(*types.Signature); ok {
		params := sig.Params()
		switch params.Len() {
		case 3:
			hasCtx = isContextType(params.At(0).Type())
			hasResolve = isResolverLike(params.At(1).Type())
		case 2:
			hasResolve = isResolverLike(params.At(0).Type())
		}
	}
	return hasCtx, hasResolve
}

/* =========================
   Low-level utils
   ========================= */

func objectOf(p *packages.Package, e ast.Expr) types.Object {
	switch v := e.(type) {
	case *ast.Ident:
		return p.TypesInfo.ObjectOf(v)
	case *ast.SelectorExpr:
		return p.TypesInfo.ObjectOf(v.Sel)
	case *ast.IndexExpr:
		return objectOf(p, v.X)
	case *ast.IndexListExpr:
		return objectOf(p, v.X)
	default:
		return nil
	}
}

func typeString(p *packages.Package, t types.Type) string {
	qf := func(pkg *types.Package) string {
		if pkg == nil || pkg.Path() == p.PkgPath {
			return ""
		}
		return pkg.Name()
	}
	return types.TypeString(t, qf)
}

func exprText(e ast.Expr) string { return types.ExprString(e) }

func pkgPathOf(p *packages.Package, e ast.Expr) string {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok {
			if obj, objOk := p.TypesInfo.ObjectOf(id).(*types.PkgName); objOk && obj.Imported() != nil {
				return obj.Imported().Path()
			}
		}
	case *ast.Ident:
		return p.PkgPath
	case *ast.IndexExpr:
		return pkgPathOf(p, v.X)
	case *ast.IndexListExpr:
		return pkgPathOf(p, v.X)
	}
	return ""
}

func funName(p *packages.Package, expr ast.Expr) (string, string) {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name, ""
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok {
			return v.Sel.Name, id.Name
		}
		return v.Sel.Name, exprText(v.X)
	case *ast.IndexExpr:
		return funName(p, v.X)
	case *ast.IndexListExpr:
		return funName(p, v.X)
	default:
		return types.ExprString(expr), ""
	}
}

func indexTypeArgText(fun ast.Expr) string {
	switch v := fun.(type) {
	case *ast.IndexExpr:
		return exprText(v.Index)
	case *ast.IndexListExpr:
		if len(v.Indices) == 1 {
			return exprText(v.Indices[0])
		}
	}
	return ""
}

func isResolverLike(t types.Type) bool {
	n, ok := t.(*types.Named)
	if !ok || n.Obj() == nil || n.Obj().Pkg() == nil {
		return false
	}
	pkg := n.Obj().Pkg()
	return pkg.Name() == pkgAssemble && (n.Obj().Name() == "Resolver" || n.Obj().Name() == "Resolve")
}

func isContextType(t types.Type) bool {
	n, ok := t.(*types.Named)
	if !ok || n.Obj() == nil || n.Obj().Pkg() == nil {
		return false
	}
	return n.Obj().Pkg().Path() == "context" && n.Obj().Name() == "Context"
}

func extractDurationMillis(_ *packages.Package, call *ast.CallExpr) int64 {
	if len(call.Args) != 1 {
		return 0
	}
	return millisFromExpr(call.Args[0])
}

func millisFromExpr(e ast.Expr) int64 {
	switch a := e.(type) {
	case *ast.BasicLit:
		return millisFromBasicLit(a)
	case *ast.SelectorExpr:
		return millisFromSelector(a)
	case *ast.BinaryExpr:
		return millisFromBinaryExpr(a)
	default:
		return 0
	}
}

func millisFromBasicLit(a *ast.BasicLit) int64 {
	if a.Kind != token.INT {
		return 0
	}
	v, _ := strconv.Atoi(a.Value)
	return int64(v)
}

func millisFromSelector(sel *ast.SelectorExpr) int64 {
	const (
		secondMillis = 1000
		minuteMillis = 60 * secondMillis
	)

	t := exprText(sel)
	switch {
	case strings.HasSuffix(t, "Millisecond"):
		return 1
	case strings.HasSuffix(t, "Second"):
		return secondMillis
	case strings.HasSuffix(t, "Minute"):
		return minuteMillis
	default:
		return 0
	}
}

func millisFromBinaryExpr(a *ast.BinaryExpr) int64 {
	const (
		secondMillis = 1000
		minuteMillis = 60 * secondMillis
	)
	if a.Op != token.MUL {
		return 0
	}
	left, ok := a.X.(*ast.BasicLit)
	if !ok || left.Kind != token.INT {
		return 0
	}
	n, err := strconv.Atoi(left.Value)
	if err != nil {
		return 0
	}
	if sel, selOk := a.Y.(*ast.SelectorExpr); selOk {
		switch millisFromSelector(sel) {
		case 1:
			return int64(n)
		case secondMillis:
			return int64(n) * secondMillis
		case minuteMillis:
			return int64(n) * minuteMillis
		}
	}
	return 0
}

func printNode(fset *token.FileSet, n ast.Node) string {
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, fset, n)
	return buf.String()
}

func isErrorType(t types.Type) bool {
	n, ok := t.(*types.Named)
	return ok && n.Obj().Name() == "error"
}

// isAllowedType accepts either `assemble.Module` or unqualified `Module` (same pkg).
func isAllowedType(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok {
			return id.Name == pkgAssemble && v.Sel.Name == typeModuleIdent
		}
	case *ast.Ident:
		return v.Name == typeModuleIdent
	}
	return false
}
