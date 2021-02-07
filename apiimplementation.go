package main

import (
	"go/types"
	"io"
	"strings"
	"text/template"
)

type APIImplementationWriter struct{ Dir string }

func NewAPIImplementationWriter(dir string) *APIImplementationWriter { return &APIImplementationWriter{Dir: dir} }

func (APIImplementationWriter) Name() string     { return "api_implementation" }
func (APIImplementationWriter) Language() string { return "go" }
func (w APIImplementationWriter) File(typeName string, _ *types.Named, _ *types.Struct) string {
	return w.Dir + "/" + strings.ToLower(typeName) + "api/" + strings.ToLower(typeName) + "api.go"
}

func (w APIImplementationWriter) PackageName(typeName string, _ *types.Named, _ *types.Struct) string {
  return strings.ToLower(typeName) + "api"
}

func (APIImplementationWriter) Imports() []string {
	return []string{
		"fknsrs.biz/p/civil",
		"fknsrs.biz/p/sqlbuilder",
		"github.com/gorilla/mux",
		"github.com/pkg/errors",
		"github.com/satori/go.uuid",
		"github.com/timewasted/go-accept-headers",
		"github.com/vmihailenco/msgpack/v5",
		"movingdata.com/p/wbi/internal/apifilter",
		"movingdata.com/p/wbi/internal/apitypes",
		"movingdata.com/p/wbi/internal/changeregistry",
		"movingdata.com/p/wbi/internal/cookiesession",
		"movingdata.com/p/wbi/internal/traceregistry",
	}
}

func (w *APIImplementationWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	model, err := makeModel(typeName, namedType, structType)
	if err != nil {
		return err
	}
	return apiImplementationTemplate.Execute(wr, *model)
}

var apiImplementationTemplate = template.Must(template.New("apiImplementationTemplate").Funcs(tplFunc).Parse(`
{{$Type := .}}

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

func (p *FilterParameters) Decode(q url.Values) error {
  return decodeStruct(q, p)
}

func (p *FilterParameters) AddFilters(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement {
  if p == nil {
    return q
  }

  a := apifilter.BuildFilters({{$Type.Singular}}Table, p)

{{range $Filter := $Type.SpecialFilters}}
  if !isNil(p.{{$Filter.GoName}}) {
    a = append(a, {{$Type.Singular}}SpecialFilter{{$Filter.GoName}}(*p.{{$Filter.GoName}}))
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

func (p *SearchParameters) Decode(q url.Values) error {
  return decodeStruct(q, p)
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
{{- if not $Field.NoOrder}}
      case "{{$Field.APIName}}":
        fld = {{$Type.Singular}}Table{{$Field.GoName}}
{{- end}}
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
