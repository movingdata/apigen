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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/imports"
)

var (
	flagLogLevel          string
	flagGoDir             string
	flagJSDir             string
	flagFlowDir           string
	flagSwaggerFile       string
	flagFilter            string
	flagGenerators        string
	flagDry               bool
	flagDisableFormatting bool
)

func init() {
	flag.StringVar(&flagLogLevel, "log_level", "info", "Log level (options are panic, fatal, error, warn, info, debug, trace).")
	flag.StringVar(&flagGoDir, "go_dir", "", "Directory to output model code to (default is the same directory as the source files).")
	flag.StringVar(&flagJSDir, "js_dir", "", "Directory to output JavaScript code to (default is ../client/src relative to the source files).")
	flag.StringVar(&flagFlowDir, "flow_dir", "", "Directory to output Flow code to (default is ../static/flow/lib relative to the source files).")
	flag.StringVar(&flagSwaggerFile, "swagger_file", "", "File to output Swagger schema to (default is ../static/swagger.json relative to the source files).")
	flag.StringVar(&flagFilter, "filter", "", "Filter to only the types in this comma-separated list.")
	flag.StringVar(&flagGenerators, "generators", "api,apifilter,enum,flow,js,schema,sql,swagger", "Run only the specified generators.")
	flag.BoolVar(&flagDry, "dry", false, "Dry run (don't write files).")
	flag.BoolVar(&flagDisableFormatting, "disable_formatting", false, "Disable formatting (if applicable).")
}

func logTime(l *logrus.Entry, s string, fn func()) {
	l = l.WithField("operation", s)

	a := time.Now()

	l = l.WithField("time_start", a)

	l.Debug("starting")

	fn()

	b := time.Now()

	l.WithFields(logrus.Fields{
		"time_end":   b,
		"time_total": b.Sub(a),
	}).Debug("finished")
}

func main() {
	flag.Parse()

	ll, err := logrus.ParseLevel(flagLogLevel)
	if err != nil {
		panic(err)
	}
	logrus.SetLevel(ll)

	l := logrus.NewEntry(logrus.StandardLogger())

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
		l.WithError(err).Fatal("couldn't load packages")
	}

	var foundErrors = false

	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		for _, err := range pkg.Errors {
			// this happens when we remove a field
			if strings.Contains(err.Error(), "_api.go:") && strings.Contains(err.Error(), "has no field or method") {
				continue
			}

			l.WithError(err).Error("error found in package")

			foundErrors = true
		}
	})

	if foundErrors {
		l.Fatal("errors found in package(s)")
	}

	generatorMap := make(map[string]bool)
	for _, s := range strings.Split(flagGenerators, ",") {
		if s == "" {
			continue
		}
		generatorMap[s] = true
	}

	for _, pkg := range pkgs {
		l := l.WithField("package", pkg.Types.Name())

		goDir := flagGoDir
		if goDir == "" {
			goDir = pkg.PkgPath
		}
		if goDir == "" {
			goDir = filepath.Dir(pkg.GoFiles[0])
		}
		if goDir == "" {
			l.Fatal("could not determine go directory")
		}

		jsDir := flagJSDir
		if jsDir == "" {
			jsDir = filepath.Join(goDir, "../client/src")
		}

		flowDir := flagFlowDir
		if flowDir == "" {
			flowDir = filepath.Join(goDir, "../static/flow/lib")
		}

		swaggerFile := flagSwaggerFile
		if swaggerFile == "" {
			swaggerFile = filepath.Join(goDir, "../static/swagger.json")
		}

		var generatorList []generator

		if len(generatorMap) == 0 || generatorMap["api"] {
			generatorList = append(generatorList, NewAPIGenerator(goDir))
		}
		if len(generatorMap) == 0 || generatorMap["apifilter"] {
			generatorList = append(generatorList, NewAPIFilterGenerator(goDir))
		}
		if len(generatorMap) == 0 || generatorMap["enum"] {
			generatorList = append(generatorList, NewEnumGenerator(goDir))
		}
		if len(generatorMap) == 0 || generatorMap["js"] {
			generatorList = append(generatorList, NewJSGenerator(jsDir))
		}
		if len(generatorMap) == 0 || generatorMap["flow"] {
			generatorList = append(generatorList, NewFlowGenerator(flowDir))
		}
		if len(generatorMap) == 0 || generatorMap["schema"] {
			generatorList = append(generatorList, NewSchemaGenerator(goDir))
		}
		if len(generatorMap) == 0 || generatorMap["sql"] {
			generatorList = append(generatorList, NewSQLGenerator(goDir))
		}
		if len(generatorMap) == 0 || generatorMap["swagger"] {
			generatorList = append(generatorList, NewSwaggerGenerator(swaggerFile))
		}

		packageName := pkg.Types.Name()
		if packageName == "" {
			l.Fatal("couldn't determine package name")
		}

		var filters []*regexp.Regexp
		if flagFilter != "" {
			for _, e := range strings.Split(flagFilter, ",") {
				filters = append(filters, regexp.MustCompile("^"+e+"$"))
			}
		}

		var models []*Model

		for _, typeName := range pkg.Types.Scope().Names() {
			l := l.WithField("model", typeName)

			if len(filters) > 0 {
				match := false

				for _, f := range filters {
					if f.MatchString(typeName) {
						match = true
						break
					}
				}

				if !match {
					continue
				}
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

			model, err := makeModel(typeName, namedType, structType)
			if err != nil {
				l.WithError(err).Fatal("could not make model object")
			}

			models = append(models, model)

			for _, g := range generatorList {
				g, ok := g.(generatorForModel)
				if !ok {
					continue
				}

				l := l.WithFields(logrus.Fields{
					"mode":      "model",
					"generator": g.Name(),
				})

				if err := executeWriters(l, g.Model(model)); err != nil {
					l.WithError(err).Fatal("could not execute writer(s)")
				}
			}
		}

		for _, g := range generatorList {
			g, ok := g.(generatorForModels)
			if !ok {
				continue
			}

			l := l.WithFields(logrus.Fields{
				"mode":      "models",
				"generator": g.Name(),
			})

			if err := executeWriters(l, g.Models(models)); err != nil {
				l.WithError(err).Fatal("could not execute writer(s)")
			}
		}
	}
}

func executeWriters(l *logrus.Entry, a []writer) error {
	for i, w := range a {
		l := l.WithField("writer", i)

		if err := executeWriter(l, w); err != nil {
			l.WithError(err).Fatal("could not execute writer")
		}
	}

	return nil
}

func executeWriter(l *logrus.Entry, w writer) error {
	filename := w.File()

	l = l.WithField("output", filename)

	l.Info("executing writer")

	if !flagDry {
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			l.WithError(err).Fatal("could not prepare target directory")
		}
	}

	buf := bytes.NewBuffer(nil)

	if w, ok := w.(writerForGo); ok {
		logTime(l, "execute go header template", func() {
			if err := headerTemplate.Execute(buf, struct {
				PackageName string
				Imports     []string
			}{w.PackageName(), w.Imports()}); err != nil {
				l.WithError(err).Fatal("could not write go header")
			}
		})
	}

	logTime(l, "generate code", func() {
		if err := w.Write(buf); err != nil {
			l.WithError(err).Fatal("could not generate code")
		}
	})

	nice := buf.Bytes()

	if w.Language() == "go" && !flagDisableFormatting {
		logTime(l, "format go code", func() {
			d, err := imports.Process(filename, nice, nil)
			if err != nil {
				l.WithError(err).Fatal("could not format go code")
			}
			nice = d
		})
	}

	if !flagDry {
		logTime(l, "write file", func() {
			if err := ioutil.WriteFile(filename, nice, 0644); err != nil {
				l.WithError(err).Fatal("could not write output")
			}
		})
	}

	return nil
}
