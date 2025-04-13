package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/singlechecker"
)

type allocateless struct{}

var a allocateless

var Analyzer = &analysis.Analyzer{
	Name: "lessallocate",
	Doc:  "Detects variables inside functions that can be moved to the global scope to reduce GC pressure",
	Run:  a.run,
}

func TestFile(pass *analysis.Pass, file *ast.File) bool {
	filename := pass.Fset.Position(file.Pos()).Filename
	base := filepath.Base(filename)
	// ignore test files
	if strings.Contains(base, "test") {
		return true
	}

	return false
}

type A struct {
	defines  []string
	tokens   []token.Pos
	lhsVars  []string
	rhsVars  []string
	funcArgs []string
}

func (a *A) String() string {
	b := strings.Builder{}

	b.WriteString("[")
	b.WriteString("defines=")
	b.WriteString(fmt.Sprintf("%v", a.defines))
	b.WriteString("]")

	return b.String()
}

func Traverse(pass *analysis.Pass, n ast.Node) bool {
	fn, ok := n.(*ast.FuncDecl)
	if !ok {
		return true
	}

	r := A{}

	for _, stmt := range fn.Body.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			if s.Tok == token.DEFINE && IsMapOrSlice(s.Rhs) {
				r.defines = append(r.defines, getVariableNames(s.Lhs)...)
				r.tokens = append(r.tokens, s.Lhs[0].Pos())
				continue
			}

			// Is the variable getting assigned to another var?
			if s.Tok == token.ASSIGN {
				r.lhsVars = append(r.lhsVars, getVariableNames(s.Lhs)...)
				parseRhs(s.Rhs, &r)
			}

		case *ast.ExprStmt:
			// Is the variable being used in a function call?
			parse(s.X, &r, false)
		}
	}

	for i, v := range r.defines {
		if slices.Contains(r.funcArgs, v) {
			continue
		}
		if slices.Contains(r.lhsVars, v) {
			continue
		}

		// Report position and variable that can be made global
		pass.Reportf(r.tokens[i], "%s can be moved to global", v)
	}

	return true
}

func (a *allocateless) run(pass *analysis.Pass) (interface{}, error) {
	for _, file := range pass.Files {
		if TestFile(pass, file) {
			continue
		}

		ast.Inspect(file, func(n ast.Node) bool {
			return Traverse(pass, n)
		})
	}
	return nil, nil
}

func parseRhs(exprs []ast.Expr, r *A) {
	for _, rhs := range exprs {
		parse(rhs, r, false)
	}
}

// if function arg is true, append the identifier to r.funcArgs
func parse(expr ast.Expr, r *A, function bool) {
	switch t := expr.(type) {
	// Check for vars in X and Y in binary expr
	case *ast.BinaryExpr:
		parse(t.X, r, function)
		parse(t.Y, r, function)
	case *ast.Ident:
		// We have found a variable
		if function {
			r.funcArgs = append(r.funcArgs, t.Name)
		} else {
			r.rhsVars = append(r.rhsVars, t.Name)
		}
	case *ast.CallExpr:
		// Check for any vars present in a function call expr
		parseFunc(t.Args, r)
	case *ast.SliceExpr:
		// Check the variable in slice expr slice[a: b: c]
		parse(t.X, r, function)

	case *ast.IndexExpr:
		// slice[a] or map[a]
		parse(t.X, r, function)

	case *ast.ParenExpr:
		// (a + b + fun(a, b))
		parse(t.X, r, function)
	default:
		// fmt.Println("DEFAULT", reflect.TypeOf(t))
	}
}

func parseFunc(exprs []ast.Expr, r *A) {
	for _, ex := range exprs {
		parse(ex, r, true)
	}
}

// Currently only supports Map or Slice definition
func IsMapOrSlice(expr []ast.Expr) bool {
	if len(expr) != 1 {
		return false
	}

	switch ex := expr[0].(type) {
	case *ast.CompositeLit:
		if _, ok := ex.Type.(*ast.MapType); ok {
			return CheckConstLiteral(ex)
		}
		if _, ok := ex.Type.(*ast.ArrayType); ok {
			return CheckConstLiteral(ex)
		}
	default:
		return false
	}
	return false
}

func CheckConstLiteral(ex *ast.CompositeLit) bool {
	elts := ex.Elts

	for _, a := range elts {
		switch t := a.(type) {
		case *ast.SelectorExpr:
		case *ast.BasicLit:
		case *ast.KeyValueExpr:
			if !BasicOrSelector(t.Key) || !BasicOrSelector(t.Value) {
				return false
			}

		default:
			return false
		}
	}
	return true
}

func BasicOrSelector(expr ast.Expr) bool {
	_, ok := expr.(*ast.BasicLit)
	if ok {
		return ok
	}

	_, ok = expr.(*ast.SelectorExpr)
	if ok {
		return ok
	}

	return false
}

func getLhsVariableName(expr []ast.Expr) []string {
	if len(expr) != 1 {
		return nil
	}

	names := []string{}

	switch ident := expr[0].(type) {
	case *ast.Ident:
		if ident.Name != "" {
			fmt.Println("identifier on lhs", ident)
			names = append(names, ident.Name)
		}
	}

	return names
}

func getVariableNames(expr []ast.Expr) []string {
	var names []string

	for _, e := range expr {
		// fmt.Println(reflect.TypeOf(e), e)
		switch ident := e.(type) {
		case *ast.Ident:
			if ident.Name != "" {
				names = append(names, ident.Name)
			}
		case *ast.ParenExpr:
			names = append(names, getVariableNames([]ast.Expr{ident.X})...)
		case *ast.IndexExpr:
			names = append(names, getVariableNames([]ast.Expr{ident.X})...)
		case *ast.IndexListExpr:
			names = append(names, getVariableNames([]ast.Expr{ident.X})...)
		case *ast.CallExpr:
			fmt.Println("H", ident.Args)
			names = append(names, getVariableNames(ident.Args)...)
		default:

		}
	}

	// fmt.Println(names)
	return names
}

func isHeapAllocated(typ types.Type) bool {
	switch t := typ.(type) {
	case *types.Slice, *types.Map, *types.Pointer, *types.Interface, *types.Chan:
		return true
	case *types.Array:
		return t.Len() > 10 // Consider large arrays as heap allocated
	case *types.Struct:
		return true // Assume structs may contain pointers
	default:
		return false
	}
}

func main() {
	singlechecker.Main(Analyzer)
}
