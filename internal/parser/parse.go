package parser

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

func ExtractModule(p *packages.Package, varName string, fset *token.FileSet) (Model, error) {
	var mdl Model
	var err error
	found := false

	for _, f := range p.Syntax {
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range vs.Names {
				if name.Name != varName || i >= len(vs.Values) {
					continue
				}
				if cl, clOk := vs.Values[i].(*ast.CompositeLit); clOk {
					mdl, err = parseModuleComposite(p, cl, fset)
					if err != nil {
						return false
					}
					found = true
					return false
				}
			}
			return true
		})
	}
	if err != nil {
		return mdl, fmt.Errorf("parsing module %q: %w", varName, err)
	}
	if !found {
		return mdl, fmt.Errorf("module %q not found", varName)
	}
	return mdl, nil
}

func parseModuleComposite(p *packages.Package, cl *ast.CompositeLit, fset *token.FileSet) (Model, error) {
	var out Model

	if !isAllowedType(cl.Type) {
		return out, fmt.Errorf("module must be of type assemble.Module (got %s)", exprText(cl.Type))
	}

	for _, elt := range cl.Elts {
		if ce, ok := elt.(*ast.CallExpr); ok {
			parseTopCall(p, ce, &out, fset)
		}
	}
	return out, nil
}

func parseTopCall(p *packages.Package, ce *ast.CallExpr, out *Model, fset *token.FileSet) {
	fnName, _ := funName(p, ce.Fun)
	switch fnName {
	case "Provide":
		if pr, ok := parseProvide(p, ce, fset); ok {
			out.Provides = append(out.Provides, pr)
		}
	case "Set":
		if st, ok := parseSet(p, ce); ok {
			out.Sets = append(out.Sets, st)
		}
	case "Bind":
		if b, ok := parseBind(p, ce); ok {
			out.Binds = append(out.Binds, b)
		}
	case "Invoke", "OnStart":
		out.Starts = append(out.Starts, parseStartLike(p, ce, fset))
	case "OnStartFor":
		out.Starts = append(out.Starts, parseStartFor(p, ce, fset))
	case "OnStop":
		out.Stops = append(out.Stops, parseStopLike(p, ce, fset))
	case "OnStopFor":
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

		// Count params/results from AST (works even if types.Info is missing)
		if a.Type != nil && a.Type.Params != nil {
			pr.LitParamCount = len(a.Type.Params.List)
		}
		if a.Type != nil && a.Type.Results != nil {
			pr.LitResultCount = len(a.Type.Results.List)
		}

		// Prefer type info if available
		if typ, ok := p.TypesInfo.Types[a]; ok && typ.Type != nil {
			if sig, sigOk := typ.Type.(*types.Signature); sigOk {
				fillProvideFromSignature(p, &pr, sig)
			}
		}

		// Heuristics if type info wasn't conclusive
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
		if !pr.TakesResolve && pr.LitParamCount == 1 && a.Type != nil && a.Type.Params != nil {
			tyTxt := exprText(a.Type.Params.List[0].Type)
			if strings.HasSuffix(tyTxt, ".Resolver") || strings.HasSuffix(tyTxt, ".Resolve") || tyTxt == "Resolver" || tyTxt == "Resolve" {
				pr.TakesResolve = true
			}
		}

	default:
		// named/selector ctor
		fillProvideFromExpr(p, &pr, a)
	}

	// options: Name("…")
	pr.Named = extractNameOption(p, ce.Args[1:])
	return pr, true
}

func parseSet(p *packages.Package, ce *ast.CallExpr) (Set, bool) {
	var st Set
	st.ElemType = indexTypeArgText(ce.Fun)

	for _, a := range ce.Args {
		call, ok := a.(*ast.CallExpr)
		if !ok {
			continue
		}
		fn, _ := funName(p, call.Fun)
		if fn != "Append" || len(call.Args) == 0 {
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
			if id, identOk := fun.X.(*ast.Ident); identOk && id.Name == "As" {
				b.ImplType = exprText(fun.Index)
			}
		case *ast.IndexListExpr:
			if id, identOk := fun.X.(*ast.Ident); identOk && id.Name == "As" && len(fun.Indices) == 1 {
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

// --- start/stop hook parsers ---

func parseStartLike(p *packages.Package, ce *ast.CallExpr, fset *token.FileSet) StartHook {
	var sh StartHook
	if len(ce.Args) > 0 {
		isLit, raw, fn, alias, path := hookCallee(p, ce.Args[0], fset)
		sh.IsFuncLit, sh.RawFuncLit, sh.FuncName, sh.PkgAlias, sh.PkgPath = isLit, raw, fn, alias, path
	}
	sh.Priority, sh.TimeoutExpr, sh.TimeoutMs = parseHookOptions(p, ce.Args[1:], "start", true)
	return sh
}

func parseStopLike(p *packages.Package, ce *ast.CallExpr, fset *token.FileSet) StopHook {
	var eh StopHook
	if len(ce.Args) > 0 {
		isLit, raw, fn, alias, path := hookCallee(p, ce.Args[0], fset)
		eh.IsFuncLit, eh.RawFuncLit, eh.FuncName, eh.PkgAlias, eh.PkgPath = isLit, raw, fn, alias, path
	}
	eh.Priority, eh.TimeoutExpr, eh.TimeoutMs = parseHookOptions(p, ce.Args[1:], "stop", true)
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
	// keep behavior: only TimeoutMs is populated for For-variants
	prio, _, ms := parseHookOptions(p, ce.Args[1:], "start", false)
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
	// keep behavior: only TimeoutMs is populated for For-variants
	prio, _, ms := parseHookOptions(p, ce.Args[1:], "stop", false)
	eh.Priority, eh.TimeoutMs = prio, ms
	return eh
}

// --- common helpers (building blocks) ---

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
		if nm != "Name" {
			continue
		}
		if lit, litOk := call.Args[0].(*ast.BasicLit); litOk && lit.Kind == token.STRING {
			v, _ := strconv.Unquote(lit.Value)
			return v
		}
	}
	return ""
}

func hookCallee(
	p *packages.Package,
	arg ast.Expr,
	fset *token.FileSet,
) (isFuncLit bool, raw, fn, alias, pkgPath string) {
	switch a := arg.(type) {
	case *ast.FuncLit:
		return true, printNode(fset, a), "", "", ""
	default:
		fn, alias = funName(p, a)
		pkgPath = pkgPathOf(p, a)
		return false, "", fn, alias, pkgPath
	}
}

// parseHookOptions parses WithPriority and With(Start|Stop)Timeout options.
// kind must be "start" or "stop". If captureExpr is true, TimeoutExpr is populated.
func parseHookOptions(p *packages.Package, args []ast.Expr, kind string, captureExpr bool) (priority int, timeoutExpr string, timeoutMs int64) {
	timeoutFn := "WithStartTimeout"
	if kind == "stop" {
		timeoutFn = "WithStopTimeout"
	}
	for _, arg := range args {
		call, ok := arg.(*ast.CallExpr)
		if !ok {
			continue
		}
		nm, _ := funName(p, call.Fun)
		switch nm {
		case "WithPriority":
			if v, prioOk := parsePriorityCall(call); prioOk {
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
		return hasCtx, hasResolve
	}

	if sig, ok := obj.Type().(*types.Signature); ok {
		params := sig.Params()
		switch params.Len() {
		case 3:
			hasCtx = isContextType(params.At(0).Type())
			hasResolve = isResolverLike(params.At(1).Type())
		case 2:
			hasCtx = false
			hasResolve = isResolverLike(params.At(0).Type())
		}
	}

	return hasCtx, hasResolve
}

// --- low-level utilities (preserved behavior) ---

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

func exprText(e ast.Expr) string {
	return types.ExprString(e)
}

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
	return pkg.Name() == "assemble" && (n.Obj().Name() == "Resolver" || n.Obj().Name() == "Resolve")
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
	switch a := call.Args[0].(type) {
	case *ast.BasicLit:
		if a.Kind == token.INT {
			v, _ := strconv.Atoi(a.Value)
			return int64(v)
		}
	case *ast.SelectorExpr:
		t := exprText(a)
		switch {
		case strings.HasSuffix(t, "Millisecond"):
			return 1
		case strings.HasSuffix(t, "Second"):
			return 1000
		case strings.HasSuffix(t, "Minute"):
			return 60 * 1000
		}
	case *ast.BinaryExpr:
		if a.Op == token.MUL {
			if left, leftOk := a.X.(*ast.BasicLit); leftOk && left.Kind == token.INT {
				n, _ := strconv.Atoi(left.Value)
				if sel, selOk := a.Y.(*ast.SelectorExpr); selOk {
					t := exprText(sel)
					switch {
					case strings.HasSuffix(t, "Millisecond"):
						return int64(n)
					case strings.HasSuffix(t, "Second"):
						return int64(n) * 1000
					case strings.HasSuffix(t, "Minute"):
						return int64(n) * 60 * 1000
					}
				}
			}
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

func isAllowedType(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok {
			return id.Name == "assemble" && v.Sel.Name == "Module"
		}
	case *ast.Ident:
		return v.Name == "Module"
	}
	return false
}
