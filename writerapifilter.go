package main

import (
	"bytes"
	"fmt"
	"go/types"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"
	"text/template"
)

type APIFilterWriter struct {
	dir    string
	pkgs   []string
	models []*Model
}

func NewAPIFilterWriter(dir string) *APIFilterWriter { return &APIFilterWriter{dir: dir} }

func (APIFilterWriter) Name() string     { return "apifilter" }
func (APIFilterWriter) Language() string { return "go" }
func (w APIFilterWriter) File(typeName string, _ *types.Named, _ *types.Struct) string {
	return w.dir + "/modelapifilter/" + strings.ToLower(typeName) + "apifilter/" + strings.ToLower(typeName) + "apifilter.go"
}

func (w APIFilterWriter) PackageName(typeName string, _ *types.Named, _ *types.Struct) string {
	return strings.ToLower(typeName) + "apifilter"
}

func (APIFilterWriter) Imports(typeName string, namedType *types.Named, structType *types.Struct) []string {
	return []string{
		"fknsrs.biz/p/civil",
		"fknsrs.biz/p/sqlbuilder",
		"github.com/pkg/errors",
		"github.com/satori/go.uuid",
		"github.com/shopspring/decimal",
		"movingdata.com/p/wbi/models/modelschema/" + strings.ToLower(typeName) + "schema",
	}
}

func (w *APIFilterWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	model, err := makeModel(typeName, namedType, structType)
	if err != nil {
		return err
	}

	w.pkgs = append(w.pkgs, w.PackageName(typeName, namedType, structType))
	w.models = append(w.models, model)

	return apifilterTemplate.Execute(wr, model)
}

func (w *APIFilterWriter) Finish(dry bool) error {
	buf := bytes.NewBuffer(nil)

	fmt.Fprintf(buf, "package modelapifilter\n\n")
	fmt.Fprintf(buf, "import (\n")
	fmt.Fprintf(buf, "  %q\n", "movingdata.com/p/wbi/models")
	for i, pkg := range w.pkgs {
		model := w.models[i]
		if len(model.SpecialFilters) == 0 {
			continue
		}
		fmt.Fprintf(buf, "  %q\n", "movingdata.com/p/wbi/models/modelapifilter/"+pkg)
	}
	fmt.Fprintf(buf, ")\n\n")
	fmt.Fprintf(buf, "func init() {\n")
	for i, pkg := range w.pkgs {
		model := w.models[i]
		for _, filter := range model.SpecialFilters {
			fmt.Fprintf(buf, "  %s.RegisterSpecialFilter%s(models.%sSpecialFilter%s)\n", filepath.Base(pkg), filter.GoName, model.Singular, filter.GoName)
		}
	}
	fmt.Fprintf(buf, "}\n")

	if !dry {
		if err := ioutil.WriteFile(w.dir+"/modelapifilter/modelapifilter.go", buf.Bytes(), 0644); err != nil {
			return err
		}
	}

	return nil
}

var apifilterTemplate = template.Must(template.New("apifilterTemplate").Funcs(tplFunc).Parse(`
{{$Type := .}}

{{range $Filter := $Type.SpecialFilters}}
var specialFilter{{$Filter.GoName}} func({{$Filter.GoType | UnPtr}}) sqlbuilder.AsExpr
func RegisterSpecialFilter{{$Filter.GoName}}(fn func({{$Filter.GoType | UnPtr}}) sqlbuilder.AsExpr) {
  specialFilter{{$Filter.GoName}} = fn
}
{{- end}}

type FilterParameters struct {
{{- range $Field := $Type.Fields}}
{{- range $Filter := $Field.Filters}}
  {{$Filter.GoName}} {{$Filter.GoType}} "schema:\"{{$Filter.Name}}\" json:\"{{$Filter.Name}},omitempty\" api_filter:\"{{$Field.SQLName}},{{$Filter.Operator}}\""
{{- end}}
{{- end}}
{{- range $Filter := $Type.SpecialFilters}}
  {{$Filter.GoName}} {{$Filter.GoType}} "schema:\"{{$Filter.Name}}\" json:\"{{$Filter.Name}},omitempty\""
{{- end}}
}

func (p *FilterParameters) AddFilters(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement {
  if p == nil {
    return q
  }

  a := apifilter.BuildFilters({{$Type.Singular | LC}}schema.Table, p)

{{range $Filter := $Type.SpecialFilters}}
{{- if eq $Filter.GoType "*uuid.UUID"}}
  if p.{{$Filter.GoName}} != nil {
{{- else if eq $Filter.GoType "*string"}}
  if p.{{$Filter.GoName}} != nil {
{{- else if eq $Filter.GoType "*int"}}
  if p.{{$Filter.GoName}} != nil {
{{- else if eq $Filter.GoType "*time.Time"}}
  if p.{{$Filter.GoName}} != nil {
{{- else}}
  if !modelutil.IsNil(p.{{$Filter.GoName}}) { // "{{$Filter.GoType}}"
{{- end}}
    a = append(a, specialFilter{{$Filter.GoName}}(*p.{{$Filter.GoName}}))
  }
{{- end}}

  if len(a) > 0 {
    q = q.AndWhere(sqlbuilder.BooleanOperator("AND", a...))
  }

  return q
}

type SearchParameters struct {
  FilterParameters
  Order *string "schema:\"order\" json:\"order,omitempty\""
  Offset *int "schema:\"offset\" json:\"offset,omitempty\""
  Limit *int "schema:\"limit\" json:\"limit,omitempty\""
}

func (p *SearchParameters) AddFilters(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement {
  if p == nil {
    return q
  }

  return p.FilterParameters.AddFilters(q)
}

func (p *SearchParameters) AddLimits(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement {
  if p.Order != nil {
    var l []sqlbuilder.AsOrderingTerm
    for _, s := range strings.Split(*p.Order, ",") {
      if len(s) < 1 {
        continue
      }

      var fld sqlbuilder.AsExpr
      desc := false
      if s[0] == '-' {
        s = s[1:]
        desc = true
      }

      switch s {
{{- range $Field := $Type.Fields}}
      case "{{$Field.APIName}}":
        fld = {{$Type.Singular | LC}}schema.Column{{$Field.GoName}}
{{- end}}
{{- range $Field := $Type.SpecialOrders}}
      case "{{$Field.APIName}}":
        l = append(l, {{$Type.Singular}}SpecialOrder{{$Field.GoName}}(desc)...)
{{- end}}
      }

      if fld != nil {
        if desc {
          l = append(l, sqlbuilder.OrderDesc(fld))
        } else {
          l = append(l, sqlbuilder.OrderAsc(fld))
        }
      }
    }

    if len(l) > 0 {
      q = q.OrderBy(l...)
    }
  }

  if p.Offset != nil && p.Limit != nil {
    q = q.OffsetLimit(sqlbuilder.OffsetLimit(sqlbuilder.Bind(*p.Offset), sqlbuilder.Bind(*p.Limit)))
  } else if p.Limit != nil {
    q = q.OffsetLimit(sqlbuilder.OffsetLimit(sqlbuilder.Bind(0), sqlbuilder.Bind(*p.Limit)))
  }

  return q
}
`))
