package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: ast-inspector <go-file-path>")
		os.Exit(1)
	}

	path := os.Args[1]
	fmt.Printf("Analyzing Decorated Syntax Tree for: %s\n\n", path)

	fset := token.NewFileSet()
	file, err := decorator.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		fmt.Printf("Failed to parse file: %v\n", err)
		os.Exit(1)
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *dst.FuncDecl:
			recvName := ""
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recvName = fmt.Sprintf("(recv %T) ", d.Recv.List[0].Type)
			}
			fmt.Printf("- Function: %s%s\n", recvName, d.Name.Name)
			if len(d.Decs.Start) > 0 {
				fmt.Printf("  Comments: %v\n", d.Decs.Start)
			}
		case *dst.GenDecl:
			fmt.Printf("- Generic Declaration (Tok: %s)\n", d.Tok)
		}
	}
}
