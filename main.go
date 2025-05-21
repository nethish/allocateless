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

type Identifiers struct {
	// Vars definied in a function or a method
	defines []string

	// Stores the Position of the identifier to report it to the console
	tokens []token.Pos

	// Vars present in LHS and RHS
	lhsVars []string
	rhsVars []string

	// Vars present in function arguments that might be modified (non-basic types)
	potentiallyModifiedFuncArgs []string
}

func (a *Identifiers) String() string {
	b := strings.Builder{}

	b.WriteString("[")
	b.WriteString("defines=")
	b.WriteString(fmt.Sprintf("%v", a.defines))
	b.WriteString("]")

	return b.String()
}

// Traverse traverses the node to find identifiers present in lhs, rhs and function calls
func Traverse(pass *analysis.Pass, n ast.Node) bool {
	fn, ok := n.(*ast.FuncDecl)
	if !ok {
		return true
	}

	r := Identifiers{}

	processStatementList(pass, fn.Body.List, &r)

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

// processStatementList iterates over a list of statements and processes them.
func processStatementList(pass *analysis.Pass, stmts []ast.Stmt, r *Identifiers) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			// Is the token a definition?
			if s.Tok == token.DEFINE && IsNewDefinition(s.Rhs) {
				r.defines = append(r.defines, getVariableNames(s.Lhs)...)
				r.tokens = append(r.tokens, s.Lhs[0].Pos())
				continue
			}

			// Is the variable getting assigned to another var?
			if s.Tok == token.ASSIGN {
				r.lhsVars = append(r.lhsVars, getVariableNames(s.Lhs)...)
				parseRhs(pass, s.Rhs, r)
			}

		case *ast.ExprStmt:
			// Is the variable being used in a function call?
			parse(pass, s.X, r, false)
		case *ast.IfStmt:
			// Process the body of the if statement
			processStatementList(pass, s.Body.List, r)
			// Process the else block, if it exists
			if s.Else != nil {
				switch elseStmt := s.Else.(type) {
				case *ast.BlockStmt:
					processStatementList(pass, elseStmt.List, r)
				case *ast.IfStmt:
					// Handle else if by processing it as a new list containing single if statement
					processStatementList(pass, []ast.Stmt{elseStmt}, r)
				}
			}
		case *ast.SwitchStmt:
			// Iterate over case clauses in the switch statement
			for _, caseClause := range s.Body.List {
				if cc, ok := caseClause.(*ast.CaseClause); ok {
					// Process the body of the case clause
					processStatementList(pass, cc.Body, r)
				}
			}
		}
	}
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

func parseRhs(pass *analysis.Pass, exprs []ast.Expr, r *Identifiers) {
	for _, rhs := range exprs {
		parse(pass, rhs, r, false)
	}
}

// if function arg is true, append the identifier to r.potentiallyModifiedFuncArgs
func parse(pass *analysis.Pass, expr ast.Expr, r *Identifiers, function bool) {
	switch t := expr.(type) {
	// Check for vars in X and Y in binary expr
	case *ast.BinaryExpr:
		parse(pass, t.X, r, function)
		parse(pass, t.Y, r, function)
	case *ast.Ident:
		// We have found a variable
		if function {
			// If it's a function argument, check its type
			if typ := pass.TypesInfo.TypeOf(t); typ != nil {
				if _, ok := typ.Underlying().(*types.Basic); !ok {
					// Not a basic type, so it might be modified
					r.potentiallyModifiedFuncArgs = append(r.potentiallyModifiedFuncArgs, t.Name)
				}
				// If it IS a basic type, we assume it's passed by value and not relevant for modification tracking here
			}
		} else {
			r.rhsVars = append(r.rhsVars, t.Name)
		}
	case *ast.CallExpr:
		// Check for any vars present in a function call expr
		parseFunc(pass, t.Args, r)
	case *ast.SliceExpr:
		// Check the variable in slice expr slice[a: b: c]
		parse(pass, t.X, r, function)

	case *ast.IndexExpr:
		// slice[a] or map[a]
		parse(pass, t.X, r, function)

	case *ast.ParenExpr:
		// (a + b + fun(a, b))
		parse(pass, t.X, r, function)
	default:
		// fmt.Println("DEFAULT", reflect.TypeOf(t))
	}
}

func parseFunc(pass *analysis.Pass, exprs []ast.Expr, r *Identifiers) {
	for _, ex := range exprs {
		parse(pass, ex, r, true)
	}
}

// Map, Slice or a Basic Literal
func IsNewDefinition(expr []ast.Expr) bool {
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
	case *ast.BasicLit:
		return true
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

// Returns true if BasicLiteral or Selector expression
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

// getVariableNames returns slice of string of the the identifiers
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

func main() {
	singlechecker.Main(Analyzer)
}
