package main

import (
  "strings"
)

type APIFilterGenerator struct {
  dir string
}

func NewAPIFilterGenerator(dir string) *APIFilterGenerator {
  return &APIFilterGenerator{dir: dir}
}

func (g *APIFilterGenerator) Name() string {
  return "apifilter"
}

func (g *APIFilterGenerator) Model(model *Model) []writer {
  return []writer{
    &basicWriterForGo{
      basicWriter: basicWriter{
        name:     "individual",
        language: "go",
        file:     g.dir + "/modelapifilter/" + strings.ToLower(model.Singular) + "apifilter/" + strings.ToLower(model.Singular) + "apifilter.go",
        write:    templateWriter(apifilterTemplate, map[string]interface{}{"Model": model}),
      },
      packageName: strings.ToLower(model.Singular) + "apifilter",
      imports: []string{
        "strings",
        "time",
        "fknsrs.biz/p/civil",
        "fknsrs.biz/p/sqlbuilder",
        "github.com/satori/go.uuid",
        "movingdata.com/p/wbi/internal/apifilter",
        "movingdata.com/p/wbi/models/modelschema/" + strings.ToLower(model.Singular) + "schema",
      },
    },
  }
}

func (g *APIFilterGenerator) Models(models []*Model) []writer {
  imports := []string{
    "movingdata.com/p/wbi/models",
  }

  for _, model := range models {
    if len(model.SpecialFilters) == 0 {
      continue
    }

    imports = append(imports, "movingdata.com/p/wbi/models/modelapifilter/"+strings.ToLower(model.Singular)+"apifilter")
  }

  return []writer{
    &basicWriterForGo{
      basicWriter: basicWriter{
        name:     "aggregated",
        language: "go",
        file:     g.dir + "/modelapifilter/modelapifilter.go",
        write:    templateWriter(apifilterFinishTemplate, map[string]interface{}{"Models": models}),
      },
      packageName: "modelapifilter",
      imports:     imports,
    },
  }
}

var apifilterTemplate = `
{{$Model := .Model}}

{{range $Filter := $Model.SpecialFilters}}
var specialFilter{{$Filter.GoName}} func({{$Filter.GoType | UnPtr}}) sqlbuilder.AsExpr
func RegisterSpecialFilter{{$Filter.GoName}}(fn func({{$Filter.GoType | UnPtr}}) sqlbuilder.AsExpr) {
  specialFilter{{$Filter.GoName}} = fn
}
{{- end}}

type FilterParameters struct {
{{- range $Field := $Model.Fields}}
{{- range $Filter := $Field.Filters}}
  {{$Filter.GoName}} {{$Filter.GoType}} "schema:\"{{$Filter.Name}}\" json:\"{{$Filter.Name}},omitempty\" api_filter:\"{{$Field.SQLName}},{{$Filter.Operator}}\""
{{- end}}
{{- end}}
{{- range $Filter := $Model.SpecialFilters}}
  {{$Filter.GoName}} {{$Filter.GoType}} "schema:\"{{$Filter.Name}}\" json:\"{{$Filter.Name}},omitempty\""
{{- end}}
}

func (p *FilterParameters) AddFilters(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement {
  if p == nil {
    return q
  }

  a := apifilter.BuildFilters({{(PackageName "schema" $Model.Singular)}}.Table, p)

{{range $Filter := $Model.SpecialFilters}}
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
{{- range $Field := $Model.Fields}}
      case "{{$Field.APIName}}":
        fld = {{(PackageName "schema" $Model.Singular)}}.Column{{$Field.GoName}}
{{- end}}
{{- range $Field := $Model.SpecialOrders}}
      case "{{$Field.APIName}}":
        l = append(l, {{$Model.Singular}}SpecialOrder{{$Field.GoName}}(desc)...)
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
`

var apifilterFinishTemplate = `
{{$Models := .Models}}

func init() {
{{- range $Model := $Models}}
{{- range $Filter := $Model.SpecialFilters}}
  {{ PackageName "apifilter" $Model.Singular }}.RegisterSpecialFilter{{$Filter.GoName}}(models.{{$Model.Singular}}SpecialFilter{{$Filter.GoName}})
{{- end}}
{{- end}}
}
`
