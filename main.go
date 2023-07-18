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

func match(pattern, value string) bool {
	pattern = strings.ReplaceAll(pattern, ".", "\\.")
	pattern = strings.ReplaceAll(pattern, "*", ".*")
	pattern = strings.ReplaceAll(pattern, "?", ".")
	match, _ := regexp.MatchString("^"+pattern+"$", value)
	return match
}

func matchAny(patterns []string, value string) bool {
	if len(patterns) == 0 {
		return true
	}

	for _, e := range patterns {
		if match(e, value) {
			return true
		}
	}

	return false
}

type filter struct {
	modelFilters  []string
	writerFilters []string
}

func (f *filter) match(modelName, writerName string) bool {
	return matchAny(f.modelFilters, modelName) && matchAny(f.writerFilters, writerName)
}

func (f *filter) String() string {
	if len(f.writerFilters) == 0 {
		return strings.Join(f.modelFilters, ",")
	}

	return strings.Join(f.modelFilters, ",") + ":" + strings.Join(f.writerFilters, ",")
}

func (f *filter) UnmarshalText(d []byte) error {
	sets := strings.Split(string(d), ":")
	if len(sets) > 2 {
		return fmt.Errorf("too many sets of filters")
	}

	if len(sets) > 0 && sets[0] != "" {
		f.modelFilters = strings.Split(sets[0], ",")
	}
	if len(sets) > 1 && sets[1] != "" {
		f.writerFilters = strings.Split(sets[1], ",")
	}

	return nil
}

func (f *filter) MarshalText() ([]byte, error) {
	return []byte(f.String()), nil
}

type filterList []filter

func (l filterList) match(modelName, writerName string) bool {
	if len(l) == 0 {
		return true
	}

	for _, e := range l {
		if e.match(modelName, writerName) {
			return true
		}
	}

	return false
}

func (l *filterList) String() string {
	return fmt.Sprint(*l)
}

func (l *filterList) Set(s string) error {
	var f filter
	if err := f.UnmarshalText([]byte(s)); err != nil {
		return err
	}

	*l = append(*l, f)

	return nil
}

var (
	flagLogLevel          string
	flagGoDir             string
	flagJSDir             string
	flagFlowDir           string
	flagFilters           filterList
	flagDry               bool
	flagDisableFormatting bool
	flagAllowSourceErrors bool
)

func init() {
	flag.StringVar(&flagLogLevel, "log_level", "info", "Log level (options are panic, fatal, error, warn, info, debug, trace).")
	flag.StringVar(&flagGoDir, "go_dir", "", "Directory to output model code to (default is the same directory as the source files).")
	flag.StringVar(&flagJSDir, "js_dir", "", "Directory to output JavaScript code to (default is ../client/src relative to the source files).")
	flag.StringVar(&flagFlowDir, "flow_dir", "", "Directory to output Flow code to (default is ../static/flow/lib relative to the source files).")
	flag.Var(&flagFilters, "filter", "Filter to only the specified models and writers.")
	flag.BoolVar(&flagDry, "dry", false, "Dry run (don't write files).")
	flag.BoolVar(&flagDisableFormatting, "disable_formatting", false, "Disable formatting (if applicable).")
	flag.BoolVar(&flagAllowSourceErrors, "allow_source_errors", false, "Don't exit when errors are found in source packages.")
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

	fmt.Printf("\n")

	var foundErrors = false

	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
	loop:
		for _, err := range pkg.Errors {
			switch {
			// this happens when we remove a field
			case strings.Contains(err.Error(), "_api.go:") && strings.Contains(err.Error(), "has no field or method"):
				continue loop
			// this happens when we remove a user filter
			case strings.Contains(err.Error(), "_api.go:") && strings.Contains(err.Error(), "undefined:") && strings.Contains(err.Error(), "UserFilter"):
				continue loop
			default:
				// nothing
			}

			l.WithError(err).Error("error found in package")

			foundErrors = true
		}
	})

	if foundErrors && !flagAllowSourceErrors {
		l.Fatal("errors found in package(s)")
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

		generatorList := []generator{
			NewAPIGenerator(goDir),
			NewAPIFilterGenerator(goDir),
			NewEnumGenerator(goDir),
			NewJSGenerator(jsDir),
			NewFlowGenerator(flowDir),
			NewSchemaGenerator(goDir),
			NewSQLGenerator(goDir),
		}

		packageName := pkg.Types.Name()
		if packageName == "" {
			l.Fatal("couldn't determine package name")
		}

		var models []*Model

		for _, typeName := range pkg.Types.Scope().Names() {
			l := l.WithField("model", typeName)

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
					"mode":      "singular",
					"generator": g.Name(),
				})

				for _, w := range g.Model(model) {
					l := l.WithField("writer", w.Name())

					if !flagFilters.match(model.Singular, g.Name()+"/"+w.Name()) {
						l.Debug("skipping writer due to not matching filter(s)")
						continue
					}

					if err := executeWriter(l, w); err != nil {
						l.WithError(err).Fatal("could not execute writer")
					}
				}
			}
		}

		for _, g := range generatorList {
			g, ok := g.(generatorForModels)
			if !ok {
				continue
			}

			l := l.WithFields(logrus.Fields{
				"mode":      "aggregated",
				"generator": g.Name(),
			})

			for _, w := range g.Models(models) {
				l := l.WithField("writer", w.Name())

				if !flagFilters.match("_", g.Name()+"/"+w.Name()) {
					l.Debug("skipping writer due to not matching filter(s)")
					continue
				}

				if err := executeWriter(l, w); err != nil {
					l.WithError(err).Fatal("could not execute writer")
				}
			}
		}
	}
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
