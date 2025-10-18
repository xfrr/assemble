package parser

type Model struct {
	Provides []Provide
	Sets     []Set
	Binds    []Bind
	Starts   []StartHook
	Stops    []StopHook
	Imports  []Import
}

type Import struct {
	Alias string // empty means default import name
	Path  string // fully-qualified import path, e.g., "github.com/xfrr/assemble"
}

type Provide struct {
	FuncName       string
	PkgPath        string
	PkgAlias       string
	IsFuncLit      bool
	RawFuncLit     string
	TakesResolve   bool
	ResType        string
	HasError       bool
	Named          string
	LitParamCount  int
	LitResultCount int
}

type Set struct {
	ElemType string
	Elems    []Provide
}

type Bind struct {
	IfaceType string
	ImplType  string
	Named     string
}

type StartHook struct {
	FuncName    string
	PkgPath     string
	PkgAlias    string
	Priority    int
	TimeoutMs   int64
	TimeoutExpr string
	IsFuncLit   bool
	RawFuncLit  string

	// OnStartFor[T]
	IsStartFor   bool
	ForType      string
	FnHasCtx     bool
	FnHasResolve bool
}

type StopHook struct {
	FuncName    string
	PkgPath     string
	PkgAlias    string
	Priority    int
	TimeoutMs   int64
	TimeoutExpr string
	IsFuncLit   bool
	RawFuncLit  string

	// OnStopFor[T]
	IsStopFor    bool
	ForType      string
	FnHasCtx     bool
	FnHasResolve bool
}
