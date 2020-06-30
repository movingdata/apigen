package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/imports"
)

var (
	packageName string
	goDir       string
	jsDir       string
	dry         bool
	filter      string
)

func init() {
	flag.StringVar(&packageName, "package_name", "", "Name of the package for the model code (default is the same as the source package).")
	flag.StringVar(&goDir, "go_dir", "", "Directory to output model code to (default is the same directory as the source files).")
	flag.StringVar(&jsDir, "js_dir", "", "Directory to output JavaScript code to (default is the ../client/src/ducks relative to the source files).")
	flag.BoolVar(&dry, "dry", false, "Dry run (don't write files).")
	flag.StringVar(&filter, "filter", "", "Filter to only the types in this comma-separated list.")
}

func main() {
	flag.Parse()

	i := 0

	cfg := packages.Config{
		Mode: packages.NeedSyntax | packages.NeedTypes | packages.NeedImports | packages.NeedDeps | packages.NeedFiles,
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			i++
			fmt.Printf("\r%d files", i)
			return parser.ParseFile(fset, filename, src, parser.ParseComments)
		},
	}

	pkgs, err := packages.Load(&cfg, flag.Args()...)
	if err != nil {
		panic(err)
	}

	fmt.Printf("\n")

	packages.PrintErrors(pkgs)

	for _, pkg := range pkgs {
		if goDir == "" {
			goDir = pkg.PkgPath
		}
		if goDir == "" {
			goDir = filepath.Dir(pkg.GoFiles[0])
		}
		if goDir == "" {
			fmt.Printf("%#v\n", pkg)
			log.Print("couldn't determine package directory")
			os.Exit(1)
		}

		if jsDir == "" {
			jsDir = filepath.Join(goDir, "../client/src/ducks")
		}

		writers := []writer{
			NewAPIWriter(goDir),
			NewSQLWriter(goDir),
			NewJSWriter(jsDir),
		}

		if packageName == "" {
			packageName = pkg.Types.Name()
		}
		if packageName == "" {
			fmt.Printf("%#v\n", pkg)
			log.Print("couldn't determine package name")
			os.Exit(1)
		}

		var filterMap map[string]bool
		if filter != "" {
			filterMap = make(map[string]bool)

			for _, e := range strings.Split(filter, ",") {
				filterMap[e] = true
			}
		}

		for _, typeName := range pkg.Types.Scope().Names() {
			if len(filterMap) > 0 && !filterMap[typeName] {
				continue
			}

			obj := pkg.Types.Scope().Lookup(typeName)

			namedType, ok := obj.Type().(*types.Named)
			if !ok {
				continue
			}

			structType, ok := namedType.Underlying().(*types.Struct)
			if !ok {
				continue
			}

			var file *ast.File
			for _, f := range pkg.Syntax {
				if obj.Pos() >= f.Pos() && obj.Pos() <= f.End() {
					file = f
					break
				}
			}

			if file == nil {
				continue
			}

			astObject := file.Scope.Lookup(obj.Name())
			if astObject == nil {
				continue
			}

			var hasComment bool

			for _, comment := range file.Comments {
				if strings.TrimSpace(comment.Text()) != "@apigen" {
					continue
				}

				if pkg.Fset.Position(comment.End()).Line == pkg.Fset.Position(obj.Pos()).Line-1 {
					hasComment = true
					break
				}
			}

			if !hasComment {
				continue
			}

			for _, w := range writers {
				filename := w.File(typeName)

				log.Printf("working on %s (%s)", typeName, filename)

				buf := bytes.NewBuffer(nil)

				if w.Language() == "go" {
					if err := headerTemplate.Execute(buf, struct {
						PackageName string
						Imports     []string
					}{packageName, w.Imports()}); err != nil {
						log.Fatalf("error writing header for %s (%s): %s", typeName, w.Name(), err.Error())
					}
				}

				if err := w.Write(buf, typeName, namedType, structType); err != nil {
					log.Fatalf("error generating code for %s (%s): %s", typeName, w.Name(), err.Error())
				}

				nice := buf.Bytes()

				if w.Language() == "go" {
					d, err := imports.Process(filename, nice, nil)
					if err != nil {
						log.Fatalf("error formatting code for %s (%s): %s", typeName, w.Name(), err.Error())
					}
					nice = d
				}

				if !dry {
					if err := ioutil.WriteFile(filename, nice, 0644); err != nil {
						log.Fatalf("error writing code for %s (%s): %s", typeName, w.Name(), err.Error())
					}
				}
			}
		}
	}
}
