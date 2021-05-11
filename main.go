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
	"time"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/imports"
)

var (
	packageName       string
	goDir             string
	jsDir             string
	swaggerFile       string
	dry               bool
	filter            string
	writers           string
	verbose           bool
	disableFormatting bool
)

func init() {
	flag.StringVar(&packageName, "package_name", "", "Name of the package for the model code (default is the same as the source package).")
	flag.StringVar(&goDir, "go_dir", "", "Directory to output model code to (default is the same directory as the source files).")
	flag.StringVar(&jsDir, "js_dir", "", "Directory to output JavaScript code to (default is ../client/src/ducks relative to the source files).")
	flag.StringVar(&swaggerFile, "swagger_file", "", "File to output Swagger schema to (default is ../static/swagger.json relative to the source files).")
	flag.BoolVar(&dry, "dry", false, "Dry run (don't write files).")
	flag.StringVar(&filter, "filter", "", "Filter to only the types in this comma-separated list.")
	flag.StringVar(&writers, "writers", "api,apifilter,enum,schema,sql,js,swagger", "Run only the specified writers.")
	flag.BoolVar(&verbose, "verbose", false, "Show timing and debug information.")
	flag.BoolVar(&disableFormatting, "disable_formatting", false, "Disable formatting (if applicable).")
}

func logTime(s string, fn func()) {
	a := time.Now()
	fn()
	b := time.Now()
	if verbose {
		fmt.Printf("%s took %s\n", s, b.Sub(a))
	}
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

	if verbose {
		packages.PrintErrors(pkgs)
	}

	writerMap := make(map[string]bool)
	for _, s := range strings.Split(writers, ",") {
		if s == "" {
			continue
		}
		writerMap[s] = true
	}

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
		if swaggerFile == "" {
			swaggerFile = filepath.Join(goDir, "../static/swagger.json")
		}

		var writerList []writer

		if len(writerMap) == 0 || writerMap["api"] {
			writerList = append(writerList, NewAPIWriter(goDir))
		}
		if len(writerMap) == 0 || writerMap["apifilter"] {
			writerList = append(writerList, NewAPIFilterWriter(goDir))
		}
		if len(writerMap) == 0 || writerMap["enum"] {
			writerList = append(writerList, NewEnumWriter(goDir))
		}
		if len(writerMap) == 0 || writerMap["schema"] {
			writerList = append(writerList, NewSchemaWriter(goDir))
		}
		if len(writerMap) == 0 || writerMap["sql"] {
			writerList = append(writerList, NewSQLWriter(goDir))
		}
		if len(writerMap) == 0 || writerMap["js"] {
			writerList = append(writerList, NewJSWriter(jsDir))
		}
		if len(writerMap) == 0 || writerMap["swagger"] {
			writerList = append(writerList, NewSwaggerWriter(swaggerFile))
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

			for _, w := range writerList {
				filename := w.File(typeName, namedType, structType)

				if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
					log.Fatalf("error preparing directory for %s (%s): %s", typeName, w.Name(), err.Error())
				}

				log.Printf("working on %s (%s)", typeName, filename)

				buf := bytes.NewBuffer(nil)

				if w.Language() == "go" {
					thisPackageName := packageName
					if n, ok := w.(packageNamer); ok {
						thisPackageName = n.PackageName(typeName, namedType, structType)
					}

					logTime("executing go header template", func() {
						if err := headerTemplate.Execute(buf, struct {
							PackageName string
							Imports     []string
						}{thisPackageName, w.Imports(typeName, namedType, structType)}); err != nil {
							log.Fatalf("error writing header for %s (%s): %s", typeName, w.Name(), err.Error())
						}
					})
				}

				logTime("executing logic", func() {
					if err := w.Write(buf, typeName, namedType, structType); err != nil {
						log.Fatalf("error generating code for %s (%s): %s", typeName, w.Name(), err.Error())
					}
				})

				nice := buf.Bytes()

				if w.Language() == "go" && !disableFormatting {
					logTime("formatting go code", func() {
						d, err := imports.Process(filename, nice, nil)
						if err != nil {
							log.Fatalf("error formatting code for %s (%s): %s", typeName, w.Name(), err.Error())
						}
						nice = d
					})
				}

				if !dry {
					logTime("writing file", func() {
						if err := ioutil.WriteFile(filename, nice, 0644); err != nil {
							log.Fatalf("error writing code for %s (%s): %s", typeName, w.Name(), err.Error())
						}
					})
				}
			}
		}

		for _, w := range writerList {
			if f, ok := w.(finisher); ok {
				logTime("finishing writer", func() {
					if err := f.Finish(dry); err != nil {
						panic(err)
					}
				})
			}
		}
	}
}
