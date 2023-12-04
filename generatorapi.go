package main

import (
  "strings"
)

type APIGenerator struct {
  dir string
}

func NewAPIGenerator(dir string) *APIGenerator {
  return &APIGenerator{dir: dir}
}

func (g *APIGenerator) Name() string {
  return "api"
}

func (g *APIGenerator) Model(model *Model) []writer {
  return []writer{
    &basicWriterForGo{
      basicWriter: basicWriter{
        name:     "individual",
        language: "go",
        file:     g.dir + "/" + strings.ToLower(model.Singular) + "_api.go",
        write:    templateWriter(apiTemplate, map[string]interface{}{"Model": model}),
      },
      packageName: "models",
      imports: []string{
        "encoding/csv",
        "encoding/json",
        "fmt",
        "fknsrs.biz/p/civil",
        "fknsrs.biz/p/sqlbuilder",
        "github.com/gorilla/mux",
        "github.com/satori/go.uuid",
        "github.com/timewasted/go-accept-headers",
        "movingdata.com/p/wbi/internal/apifilter",
        "movingdata.com/p/wbi/internal/changeregistry",
        "movingdata.com/p/wbi/internal/cookiesession",
        "movingdata.com/p/wbi/internal/modelutil",
        "movingdata.com/p/wbi/internal/modelrelations",
        "movingdata.com/p/wbi/internal/retrydb",
        "movingdata.com/p/wbi/internal/traceregistry",
        "movingdata.com/p/wbi/models/modelapifilter/" + strings.ToLower(model.Singular) + "apifilter",
        "movingdata.com/p/wbi/models/modelenum/" + strings.ToLower(model.Singular) + "enum",
        "movingdata.com/p/wbi/models/modelschema/" + strings.ToLower(model.Singular) + "schema",
      },
    },
  }
}

var apiTemplate = `
{{$Model := .Model}}

// Please note: this file is generated from {{$Model.Singular | LC}}.go

func init() {
  modelutil.RegisterFinder("{{$Model.Singular}}", func(ctx context.Context, db modelutil.RowQueryerContext, id interface{}, uid, euid *uuid.UUID) (interface{}, error) {
    idValue, ok := id.({{$Model.IDField.GoType}})
    if !ok {
      return nil, fmt.Errorf("{{$Model.Singular}}: id should be {{$Model.IDField.GoType}}; was instead %T", id)
    }

    v, err := {{$Model.Singular}}APIGet(ctx, db, idValue, uid, euid)
    if err != nil {
      return nil, err
    }
    if v == nil {
      return nil, nil
    }
    return v, nil
  })
}

{{range $Field := $Model.Fields}}
{{- if $Field.Enum}}
func (jsctx *JSContext) {{$Model.Singular}}EnumValid{{$Field.GoName}}(v string) bool {
  return {{(PackageName "enum" $Model.Singular)}}.Valid{{$Field.GoName}}[v]
}

func (jsctx *JSContext) {{$Model.Singular}}EnumValues{{$Field.GoName}}() []string {
  return {{(PackageName "enum" $Model.Singular)}}.Values{{$Field.GoName}}
}

func (jsctx *JSContext) {{$Model.Singular}}EnumLabel{{$Field.GoName}}(v string) string {
  return {{(PackageName "enum" $Model.Singular)}}.Labels{{$Field.GoName}}[v]
}
{{- end}}
{{end}}

func (jsctx *JSContext) {{$Model.Singular}}Get(id {{$Model.IDField.GoType}}) *{{$Model.Singular}} {
  v, err := {{$Model.Singular}}APIGet(jsctx.ctx, jsctx.tx, id, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (v *{{$Model.Singular}}) APIGet(ctx context.Context, db modelutil.RowQueryerContext, id {{$Model.IDField.GoType}}, uid, euid *uuid.UUID) error {
  vv, err := {{$Model.Singular}}APIGet(ctx, db, id, uid, euid)
  if err != nil {
    return fmt.Errorf("{{$Model.Singular}}.APIGet: %w", err)
  } else if vv == nil {
    return fmt.Errorf("{{$Model.Singular}}.APIGet: could not find record {{$Model.IDField.FormatType}}", id)
  }

  *v = *vv

  return nil
}

func {{$Model.Singular}}APIGet(ctx context.Context, db modelutil.RowQueryerContext, id {{$Model.IDField.GoType}}, uid, euid *uuid.UUID) (*{{$Model.Singular}}, error) {
  qb := sqlbuilder.Select().From({{(PackageName "schema" $Model.Singular)}}.Table).Columns(modelutil.ColumnsAsExpressions({{(PackageName "schema" $Model.Singular)}}.Columns)...)

{{if $Model.HasUserFilter}}
  qb = {{$Model.Singular}}UserFilter(qb, euid)
{{- end}}

  qb = qb.AndWhere(sqlbuilder.Eq({{(PackageName "schema" $Model.Singular)}}.ColumnID, sqlbuilder.Bind(id)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APIGet: couldn't generate query: %w", err)
  }

  var v {{$Model.Singular}}
{{range $Field := $Model.Fields}}
  {{if $Field.ScanType}}var x{{$Field.GoName}} {{$Field.ScanType}}{{end}}
{{- end}}
  if err := db.QueryRowContext(ctx, qs, qv...).Scan({{range $i, $Field := $Model.Fields}}{{if $Field.ScanType}}&x{{$Field.GoName}}{{else if $Field.Array}}pq.Array(&v.{{$Field.GoName}}){{else}}&v.{{$Field.GoName}}{{end}}, {{end}}); err != nil {
    if err == sql.ErrNoRows {
      return nil, nil
    }

    return nil, fmt.Errorf("{{$Model.Singular}}APIGet: couldn't perform query: %w", err)
  }

{{range $Field := $Model.Fields}}
  {{if $Field.ScanType}}v.{{$Field.GoName}} = ({{$Field.GoType}})(x{{$Field.GoName}}){{end}}
{{- end}}

  return &v, nil
}

func {{$Model.Singular}}APIHandleGet(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid *uuid.UUID) {
  vars := mux.Vars(r)

{{if (EqualStrings $Model.IDField.GoType "int")}}
  idNumber, err := strconv.ParseInt(vars["id"], 10, 64)
  if err != nil {
    panic(err)
  }
  id := int(idNumber)
{{else}}
  id, err := uuid.FromString(vars["id"])
  if err != nil {
    panic(err)
  }
{{end}}

  v, err := {{$Model.Singular}}APIGet(r.Context(), db, id, uid, euid)
  if err != nil {
    panic(err)
  }

  if v == nil {
    http.Error(rw, fmt.Sprintf("{{$Model.Singular}} with id {{FormatTemplate $Model.IDField.GoType}} not found", id), http.StatusNotFound)
    return
  }

  rw.Header().Set("content-type", "application/json")
  rw.WriteHeader(http.StatusOK)

  enc := json.NewEncoder(rw)
  if r.URL.Query().Get("_pretty") != "" {
    enc.SetIndent("", "  ")
  }

  if err := enc.Encode(v); err != nil {
    panic(err)
  }
}

type {{$Model.Singular}}APISearchResponse struct {
  Records []*{{$Model.Singular}} "json:\"records\""
  Total int "json:\"total\""
  Time time.Time "json:\"time\""
}

func (jsctx *JSContext) {{$Model.Singular}}Search(p {{(PackageName "apifilter" $Model.Singular)}}.SearchParameters) *{{$Model.Singular}}APISearchResponse {
  v, err := {{$Model.Singular}}APISearch(jsctx.ctx, jsctx.tx, &p, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func {{$Model.Singular}}APISearch(ctx context.Context, db modelutil.QueryerContextAndRowQueryerContext, p *{{(PackageName "apifilter" $Model.Singular)}}.SearchParameters, uid, euid *uuid.UUID) (*{{$Model.Singular}}APISearchResponse, error) {
  qb := sqlbuilder.Select().From({{(PackageName "schema" $Model.Singular)}}.Table).Columns(modelutil.ColumnsAsExpressions({{(PackageName "schema" $Model.Singular)}}.Columns)...)

{{- if $Model.HasUserFilter}}
  qb = {{$Model.Singular}}UserFilter(qb, euid)
{{- end}}

  qb = p.AddFilters(qb)

  qb1 := p.AddLimits(qb)
  qs1, qv1, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb1.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APISearch: couldn't generate result query: %w", err)
  }

  qb2 := qb.Columns(sqlbuilder.Func("count", sqlbuilder.Literal("*")))
  qs2, qv2, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb2.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APISearch: couldn't generate summary query: %w", err)
  }

  rows, err := db.QueryContext(ctx, qs1, qv1...)
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APISearch: couldn't perform result query: %w", err)
  }
  defer rows.Close()

  a := make([]*{{$Model.Singular}}, 0)
  for rows.Next() {
    var m {{$Model.Singular}}
{{range $Field := $Model.Fields}}
    {{if $Field.ScanType}}var x{{$Field.GoName}} {{$Field.ScanType}}{{end}}
{{- end}}
    if err := rows.Scan({{range $i, $Field := $Model.Fields}}{{if $Field.ScanType}}&x{{$Field.GoName}}{{else if $Field.Array}}pq.Array(&m.{{$Field.GoName}}){{else}}&m.{{$Field.GoName}}{{end}} /* {{$i}} */, {{end}}); err != nil {
      return nil, fmt.Errorf("{{$Model.Singular}}APISearch: couldn't scan result row: %w", err)
    }

{{range $Field := $Model.Fields}}
    {{if $Field.ScanType}}m.{{$Field.GoName}} = ({{$Field.GoType}})(x{{$Field.GoName}}){{end}}
{{- end}}

    a = append(a, &m)
  }

  if err := rows.Close(); err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APISearch: couldn't close result row set: %w", err)
  }

  var total int
  if err := db.QueryRowContext(ctx, qs2, qv2...).Scan(&total); err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APISearch: couldn't perform summary query: %w", err)
  }

  return &{{$Model.Singular}}APISearchResponse{
    Records: a,
    Total: total,
    Time: time.Now(),
  }, nil
}

func (jsctx *JSContext) {{$Model.Singular}}Find(p {{(PackageName "apifilter" $Model.Singular)}}.FilterParameters) *{{$Model.Singular}} {
  v, err := {{$Model.Singular}}APIFind(jsctx.ctx, jsctx.tx, &p, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func {{$Model.Singular}}APIFind(ctx context.Context, db modelutil.QueryerContextAndRowQueryerContext, p *{{(PackageName "apifilter" $Model.Singular)}}.FilterParameters, uid, euid *uuid.UUID) (*{{$Model.Singular}}, error) {
  qb := sqlbuilder.Select().From({{(PackageName "schema" $Model.Singular)}}.Table).Columns(modelutil.ColumnsAsExpressions({{(PackageName "schema" $Model.Singular)}}.Columns)...)

{{- if $Model.HasUserFilter}}
  qb = {{$Model.Singular}}UserFilter(qb, euid)
{{- end}}

  qb = p.AddFilters(qb)

  qs1, qv1, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APIFind: couldn't generate result query: %w", err)
  }

  qb2 := qb.Columns(sqlbuilder.Func("count", sqlbuilder.Literal("*")))
  qs2, qv2, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb2.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APIFind: couldn't generate summary query: %w", err)
  }

  var m {{$Model.Singular}}
{{range $Field := $Model.Fields}}
  {{if $Field.ScanType}}var x{{$Field.GoName}} {{$Field.ScanType}}{{end}}
{{- end}}
  if err := db.QueryRowContext(ctx, qs1, qv1...).Scan({{range $i, $Field := $Model.Fields}}{{if $Field.Array}}pq.Array(&m.{{$Field.GoName}}){{else}}&m.{{$Field.GoName}}{{end}}, {{end}}); err != nil {
    if err == sql.ErrNoRows {
      return nil, nil
    }

    return nil, fmt.Errorf("{{$Model.Singular}}APIFind: couldn't scan result row: %w", err)
  }

{{range $Field := $Model.Fields}}
  {{if $Field.ScanType}}m.{{$Field.GoName}} = ({{$Field.GoType}})(x{{$Field.GoName}}){{end}}
{{- end}}

  var total int
  if err := db.QueryRowContext(ctx, qs2, qv2...).Scan(&total); err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APIFind: couldn't perform summary query: %w", err)
  }

  if total != 1 {
    return nil, fmt.Errorf("{{$Model.Singular}}APIFind: expected one result, got %d", total)
  }

  return &m, nil
}

func (jsctx *JSContext) {{$Model.Singular}}AggregateCount(fieldNames []string, p {{(PackageName "apifilter" $Model.Singular)}}.FilterParameters) *modelutil.AggregateCountResult {
  items, err := {{$Model.Singular}}APIAggregateCount(jsctx.ctx, jsctx.tx, fieldNames, &p, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return items
}

func {{$Model.Singular}}APIAggregateCount(ctx context.Context, db modelutil.QueryerContextAndRowQueryerContext, fieldNames []string, p *{{(PackageName "apifilter" $Model.Singular)}}.FilterParameters, uid, euid *uuid.UUID) (*modelutil.AggregateCountResult, error) {
  var fields []*apitypes.Field
  var columns []sqlbuilder.AsExpr
  for _, fieldName := range fieldNames {
    switch fieldName {
{{- range $Field := $Model.Fields}}
{{- if $Field.Enum}}
    case "{{$Field.GoName}}", "{{$Field.APIName}}":
      fields = append(fields, {{(PackageName "schema" $Model.Singular)}}.Field{{$Field.GoName}})
      columns = append(columns, {{(PackageName "schema" $Model.Singular)}}.Column{{$Field.GoName}})
{{- end}}
{{- end}}
    default:
      return nil, fmt.Errorf("{{$Model.Singular}}APIAggregateCount: field %q does not exist or is not countable")
    }
  }

  qb := sqlbuilder.Select().From({{(PackageName "schema" $Model.Singular)}}.Table).Columns(
    append(columns[:], sqlbuilder.Func("count", sqlbuilder.Literal("*")))...
  ).GroupBy(columns...)

{{- if $Model.HasUserFilter}}
  qb = {{$Model.Singular}}UserFilter(qb, euid)
{{- end}}

  qb = p.AddFilters(qb)

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APIAggregateCount: couldn't generate query: %w", err)
  }

  rows, err := db.QueryContext(ctx, qs, qv...)
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APIAggregateCount: couldn't perform result query: %w", err)
  }
  defer rows.Close()

  items := []modelutil.AggregateCountItem{}

  for rows.Next() {
    values := make([]string, len(fields))
    var count int

    out := make([]interface{}, len(fields)+1)
    for i := range values {
      out[i] = &values[i]
    }
    out[len(out)-1] = &count

    if err := rows.Scan(out...); err != nil {
      return nil, fmt.Errorf("{{$Model.Singular}}APIAggregateCount: couldn't scan output row: %w", err)
    }

    items = append(items, modelutil.AggregateCountItem{Values: values, Count: count})
  }

  if err := rows.Close(); err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APIAggregateCount: couldn't close row set: %w", err)
  }

  return &modelutil.AggregateCountResult{Fields: fields, Items: items}, nil
}

{{range $Field := $Model.Fields}}
{{- if $Field.Enum}}
func (jsctx *JSContext) {{$Model.Singular}}Count{{$Field.GoName}}(p {{(PackageName "apifilter" $Model.Singular)}}.FilterParameters) map[string]int {
  counts, err := {{$Model.Singular}}APICount{{$Field.GoName}}(jsctx.ctx, jsctx.tx, &p, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return counts
}

func {{$Model.Singular}}APICount{{$Field.GoName}}(ctx context.Context, db modelutil.QueryerContextAndRowQueryerContext, p *{{(PackageName "apifilter" $Model.Singular)}}.FilterParameters, uid, euid *uuid.UUID) (map[string]int, error) {
  qb := sqlbuilder.Select().From({{(PackageName "schema" $Model.Singular)}}.Table).Columns(
    {{(PackageName "schema" $Model.Singular)}}.Column{{$Field.GoName}},
    sqlbuilder.Func("count", sqlbuilder.Literal("*")),
  ).GroupBy({{(PackageName "schema" $Model.Singular)}}.Column{{$Field.GoName}})

{{- if $Model.HasUserFilter}}
  qb = {{$Model.Singular}}UserFilter(qb, euid)
{{- end}}

  qb = p.AddFilters(qb)

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APICount{{$Field.GoName}}: couldn't generate query: %w", err)
  }

  rows, err := db.QueryContext(ctx, qs, qv...)
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APICount{{$Field.GoName}}: couldn't perform result query: %w", err)
  }
  defer rows.Close()

  counts := make(map[string]int)

  for rows.Next() {
    var value string
    var count int

    if err := rows.Scan(&value, &count); err != nil {
      return nil, fmt.Errorf("{{$Model.Singular}}APICount{{$Field.GoName}}: couldn't scan output row: %w", err)
    }

    counts[value] = count
  }

  if err := rows.Close(); err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APICount{{$Field.GoName}}: couldn't close row set: %w", err)
  }

  return counts, nil
}
{{- end}}
{{end}}

func {{$Model.Singular}}APIHandleSearch(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid *uuid.UUID) {
  var p {{(PackageName "apifilter" $Model.Singular)}}.SearchParameters
  if err := modelutil.DecodeStruct(r.URL.Query(), &p); err != nil {
    panic(err)
  }

  v, err := {{$Model.Singular}}APISearch(r.Context(), db, &p, uid, euid)
  if err != nil {
    panic(err)
  }

  rw.Header().Set("content-type", "application/json")
  rw.WriteHeader(http.StatusOK)

  enc := json.NewEncoder(rw)
  if r.URL.Query().Get("_pretty") != "" {
    enc.SetIndent("", "  ")
  }

  if err := enc.Encode(v); err != nil {
    panic(err)
  }
}

func {{$Model.Singular}}APIHandleSearchCSV(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid *uuid.UUID) {
  var p {{(PackageName "apifilter" $Model.Singular)}}.SearchParameters
  if err := modelutil.DecodeStruct(r.URL.Query(), &p); err != nil {
    panic(err)
  }

  v, err := {{$Model.Singular}}APISearch(r.Context(), db, &p, uid, euid)
  if err != nil {
    panic(err)
  }

  rw.Header().Set("content-type", "text/csv")
  rw.Header().Set("content-disposition", "attachment;filename={{$Model.Plural}} Search Results.csv")
  rw.WriteHeader(http.StatusOK)

  wr := csv.NewWriter(rw)

  if err := wr.Write([]string{ {{range $Field := $Model.Fields}}"{{$Field.GoName | UCLS}}",{{end}} }); err != nil {
    panic(err)
  }

  for _, e := range v.Records {
    if err := wr.Write([]string{ {{range $Field := $Model.Fields}}fmt.Sprintf("%v", e.{{$Field.GoName}}),{{end}} }); err != nil {
      panic(err)
    }
  }

  wr.Flush()
}

{{if (or $Model.HasAPICreate $Model.HasAPIUpdate)}}
type {{$Model.Singular}}FieldMask struct {
{{range $Field := $Model.Fields}}
  {{$Field.GoName}} bool
{{- end}}
}

func (m {{$Model.Singular}}FieldMask) ModelName() string {
  return "{{$Model.Singular}}"
}

func (m {{$Model.Singular}}FieldMask) Fields() []string {
  return modelutil.FieldMaskTrueFields("{{$Model.Singular}}", m)
}

func (m {{$Model.Singular}}FieldMask) Union(other {{$Model.Singular}}FieldMask) {{$Model.Singular}}FieldMask {
  var out {{$Model.Singular}}FieldMask
  modelutil.FieldMaskUnion(m, other, &out)
  return out
}

func (m {{$Model.Singular}}FieldMask) Intersect(other {{$Model.Singular}}FieldMask) {{$Model.Singular}}FieldMask {
  var out {{$Model.Singular}}FieldMask
  modelutil.FieldMaskIntersect(m, other, &out)
  return out
}

func (m {{$Model.Singular}}FieldMask) Match(a, b *{{$Model.Singular}}) bool {
  return modelutil.FieldMaskMatch(m, a, b)
}

func (m *{{$Model.Singular}}FieldMask) From(a, b *{{$Model.Singular}}) {
  modelutil.FieldMaskFrom(a, b, m)
}

func (m {{$Model.Singular}}FieldMask) Changes(a, b *{{$Model.Singular}}) ([]traceregistry.Change) {
  return modelutil.FieldMaskChanges(m, a, b)
}

func {{$Model.Singular}}FieldMaskFrom(a, b *{{$Model.Singular}}) {{$Model.Singular}}FieldMask {
  var m {{$Model.Singular}}FieldMask
  m.From(a, b)
  return m
}

type {{$Model.Singular}}BeforeSaveHandlerFunc func(ctx context.Context, tx *sql.Tx, uid, euid uuid.UUID, options *modelutil.APIOptions, current, proposed *{{$Model.Singular}}) error

type {{$Model.Singular}}BeforeSaveHandler struct {
  Name string
  Trigger *{{$Model.Singular}}FieldMask
  Change *{{$Model.Singular}}FieldMask
  Read []modelutil.FieldMask
  Write []modelutil.FieldMask
  DeferredRead []modelutil.FieldMask
  DeferredWrite []modelutil.FieldMask
  Func {{$Model.Singular}}BeforeSaveHandlerFunc
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetName() string {
  return h.Name
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetModelName() string {
  return "{{$Model.Singular}}"
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetQualifiedName() string {
  return "{{$Model.Singular}}." + h.GetName()
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetTriggers() []string {
  if h.Trigger != nil {
    return h.Trigger.Fields()
  }

  return []string{ {{range $Field := $Model.Fields}}"{{$Model.Singular}}.{{$Field.GoName}}",{{end}} }
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetTriggerMask() modelutil.FieldMask {
  return h.Trigger
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetChanges() []string {
  if h.Change != nil {
    return h.Change.Fields()
  }

  return []string{ {{range $Field := $Model.Fields}}"{{$Model.Singular}}.{{$Field.GoName}}",{{end}} }
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetChangeMask() modelutil.FieldMask {
  return h.Change
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetReads() []string {
  var a []string

  for _, e := range h.Read {
    a = append(a, e.Fields()...)
  }

  return a
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetReadMasks() []modelutil.FieldMask {
  return h.Read
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetWrites() []string {
  var a []string

  for _, e := range h.Write {
    a = append(a, e.Fields()...)
  }

  return a
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetWriteMasks() []modelutil.FieldMask {
  return h.Write
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetDeferredReads() []string {
  var a []string

  for _, e := range h.DeferredRead {
    a = append(a, e.Fields()...)
  }

  return a
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetDeferredReadMasks() []modelutil.FieldMask {
  return h.DeferredRead
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetDeferredWrites() []string {
  var a []string

  for _, e := range h.DeferredWrite {
    a = append(a, e.Fields()...)
  }

  return a
}

func (h {{$Model.Singular}}BeforeSaveHandler) GetDeferredWriteMasks() []modelutil.FieldMask {
  return h.DeferredWrite
}

func (h *{{$Model.Singular}}BeforeSaveHandler) Match(a, b *{{$Model.Singular}}) bool {
  if h.Trigger == nil {
    return true
  }

  return h.Trigger.Match(a, b)
}
{{end}}

{{if $Model.HasAPICreate}}
func (jsctx *JSContext) {{$Model.Singular}}Create(input {{$Model.Singular}}) *{{$Model.Singular}} {
  v, err := {{$Model.Singular}}APICreate(modelutil.WithPathEntry(jsctx.ctx, fmt.Sprintf("JS#{{$Model.Singular}}Create#{{FormatTemplate $Model.IDField.GoType}}", input.ID)), jsctx.mctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), &input, nil)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (jsctx *JSContext) {{$Model.Singular}}CreateWithOptions(input {{$Model.Singular}}, options modelutil.APIOptions) *{{$Model.Singular}} {
  v, err := {{$Model.Singular}}APICreate(modelutil.WithPathEntry(jsctx.ctx, fmt.Sprintf("JS#{{$Model.Singular}}CreateWithOptions#{{FormatTemplate $Model.IDField.GoType}}", input.ID)), jsctx.mctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), &input, &options)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (v *{{$Model.Singular}}) APICreate(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, uid, euid uuid.UUID, now time.Time, options *modelutil.APIOptions) error {
  vv, err := {{$Model.Singular}}APICreate(ctx, mctx, tx, uid, euid, now, v, options)
  if err != nil {
    return fmt.Errorf("{{$Model.Singular}}.APICreate: %w", err)
  } else if vv == nil {
    return fmt.Errorf("{{$Model.Singular}}.APICreate: {{$Model.Singular}}APICreate did not return a valid record")
  }

  *v = *vv

  return nil
}

func {{$Model.Singular}}APICreate(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, uid, euid uuid.UUID, now time.Time, input *{{$Model.Singular}}, options *modelutil.APIOptions) (*{{$Model.Singular}}, error) {
  if input.ID == {{if (EqualStrings $Model.IDField.GoType "int")}}0{{else}}uuid.Nil{{end}} {
    return nil, fmt.Errorf("{{$Model.Singular}}APICreate: ID field was empty")
  }

  ctx, queue := modelutil.WithDeferredCallbackQueue(ctx)
  ctx, log := modelutil.WithCallbackHistoryLog(ctx)
  ctx = modelutil.WithPathEntry(ctx, fmt.Sprintf("API#{{$Model.Singular}}Create#{{FormatTemplate $Model.IDField.GoType}}", input.ID))

  ic := sqlbuilder.InsertColumns{}

{{if $Model.HasAudit}}
  fields := make(map[string][]interface{})
{{- end}}

{{range $Field := $Model.Fields}}
{{- if $Field.Enum}}
{{- if $Field.Array}}
  for i, v := range input.{{$Field.GoName}} {
    if !{{(PackageName "enum" $Model.Singular)}}.Valid{{$Field.GoName}}[v] {
      return nil, fmt.Errorf("{{$Model.Singular}}APICreate: value for member %d of field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", i, {{(PackageName "enum" $Model.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
    }
  }
{{- else}}
  if !{{(PackageName "enum" $Model.Singular)}}.Valid{{$Field.GoName}}[input.{{$Field.GoName}}] {
    return nil, fmt.Errorf("{{$Model.Singular}}APICreate: value for field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", {{(PackageName "enum" $Model.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
  }
{{- end}}
{{- end}}
{{end}}

{{range $Field := $Model.Fields}}
{{- if not (eq $Field.Sequence "")}}
  if input.{{$Field.GoName}} == 0 {
    if err := tx.QueryRowContext(ctx, "select nextval('{{$Field.Sequence}}')").Scan(&input.{{$Field.GoName}}); err != nil {
      return nil, fmt.Errorf("{{$Model.Singular}}APICreate: couldn't get sequence value for field \"{{$Field.APIName}}\" from sequence \"{{$Field.Sequence}}\": %w", err)
    }
  }
{{- end}}
{{end}}

{{range $Field := $Model.Fields}}
{{- if $Field.Array}}
  if input.{{$Field.GoName}} == nil {
    input.{{$Field.GoName}} = make({{$Field.GoType}}, 0)
  }
{{- end}}
{{end}}

{{- if $Model.HasID}}
  ic[{{(PackageName "schema" $Model.Singular)}}.ColumnID] = sqlbuilder.Bind(input.ID)
{{- if $Model.HasAudit}}
  fields["ID"] = []interface{}{input.ID}
{{- end}}
{{- end}}
{{- if $Model.HasCreatedAt}}
  input.CreatedAt = now
  ic[{{(PackageName "schema" $Model.Singular)}}.ColumnCreatedAt] = sqlbuilder.Bind(input.CreatedAt)
{{- if $Model.HasAudit}}
  fields["CreatedAt"] = []interface{}{input.CreatedAt}
{{- end}}
{{- end}}
{{- if $Model.HasUpdatedAt}}
  input.UpdatedAt = now
  ic[{{(PackageName "schema" $Model.Singular)}}.ColumnUpdatedAt] = sqlbuilder.Bind(input.UpdatedAt)
{{- if $Model.HasAudit}}
  fields["UpdatedAt"] = []interface{}{input.UpdatedAt}
{{- end}}
{{- end}}
{{- if $Model.HasCreatorID}}
  input.CreatorID = euid
  ic[{{(PackageName "schema" $Model.Singular)}}.ColumnCreatorID] = sqlbuilder.Bind(input.CreatorID)
{{- if $Model.HasAudit}}
  fields["CreatorID"] = []interface{}{input.CreatorID}
{{- end}}
{{- end}}
{{- if $Model.HasUpdaterID}}
  input.UpdaterID = euid
  ic[{{(PackageName "schema" $Model.Singular)}}.ColumnUpdaterID] = sqlbuilder.Bind(input.UpdaterID)
{{- if $Model.HasAudit}}
  fields["UpdaterID"] = []interface{}{input.UpdaterID}
{{- end}}
{{- end}}
{{- if $Model.HasVersion}}
  switch input.Version {
  case 0:
    // initialise to 1 if not supplied
    input.Version = 1
  case 1:
    // nothing
  default:
    return nil, fmt.Errorf("{{$Model.Singular}}APICreate: Version from input should be 0 or 1; was instead %d: %w", input.Version, ErrVersionMismatch)
  }
  ic[{{(PackageName "schema" $Model.Singular)}}.ColumnVersion] = sqlbuilder.Bind(input.Version)
{{- if $Model.HasAudit}}
  fields["Version"] = []interface{}{input.Version}
{{- end}}
{{- end}}

  exitActivity := traceregistry.Enter(ctx, &traceregistry.EventModelActivity{
    ID: uuid.Must(uuid.NewV4()),
    Time: time.Now(),
    Action: "create",
    ModelType: "{{$Model.Singular}}",
    ModelID: input.ID,
    ModelData: input,
    Path: modelutil.GetPath(ctx),
  })
  defer func() { exitActivity() }()

  b := {{$Model.Singular}}{}

  n := 0

  for {
    m := {{$Model.Singular}}FieldMaskFrom(&b, input)
    if m == ({{$Model.Singular}}FieldMask{}) {
      break
    }
    c := b
    b = *input

    n++
    if n > 100 {
      return nil, fmt.Errorf("{{$Model.Singular}}APICreate: BeforeSave callback for %s exceeded execution limit of 100 iterations", input.ID)
    }

    exitIteration := traceregistry.Enter(ctx, &traceregistry.EventIteration{
      ID: uuid.Must(uuid.NewV4()),
      Time: time.Now(),
      ObjectType: "{{$Model.Singular}}",
      ObjectID: input.ID,
      Number: n,
    })
    defer func() { exitIteration() }()

    for _, e := range mctx.GetHandlers() {
      h, ok := e.({{$Model.Singular}}BeforeSaveHandler)
      if !ok {
        continue
      }

      skipped := false
      forced := false

      if options != nil {
        if options.SkipCallbacks.MatchConsume("{{$Model.Singular}}", h.GetName(), input.ID) {
          skipped = true
        }
        if options.ForceCallbacks.MatchConsume("{{$Model.Singular}}", h.GetName(), input.ID) {
          forced = true
        }
      }

      triggered := m
      if h.Trigger != nil {
        triggered = h.Trigger.Intersect(m)
      }

      if triggered == ({{$Model.Singular}}FieldMask{}) && !forced {
        continue
      }

      before := time.Now()

      triggerChanges := triggered.Changes(&c, input)

      exitCallback := traceregistry.Enter(ctx, &traceregistry.EventCallback{
        ID: uuid.Must(uuid.NewV4()),
        Time: before,
        Name: h.GetQualifiedName(),
        Skipped: skipped,
        Forced: forced,
        Triggered: triggerChanges,
      })
      defer func() { exitCallback() }()

      a := *input

      if !skipped || forced {
        if log != nil {
          log.Add("{{$Model.Singular}}", h.GetName(), input.ID)
        }

        if err := h.Func(modelutil.WithPathEntry(ctx, fmt.Sprintf("CB#"+h.GetQualifiedName()+"#{{FormatTemplate $Model.IDField.GoType}}", input.ID)), tx, uid, euid, options, &c, input); err != nil {
          return nil, fmt.Errorf("{{$Model.Singular}}APICreate: BeforeSave callback %s for %s failed: %w", h.Name, input.ID, err)
        }
      }

      traceregistry.Add(ctx, traceregistry.EventCallbackComplete{
        ID: uuid.Must(uuid.NewV4()),
        Time: time.Now(),
        Name: h.GetQualifiedName(),
        Duration: time.Now().Sub(before),
        Changed: {{$Model.Singular}}FieldMaskFrom(&a, input).Changes(&a, input),
      })

      exitCallback()
    }

    exitIteration()
  }

{{range $Field := $Model.Fields}}
  {{- if (and $Field.Array (not $Field.IsNull))}}
    if input.{{$Field.GoName}} == nil {
      input.{{$Field.GoName}} = make({{$Field.GoType}}, 0)
    }
  {{- end}}

  {{- if not $Field.IgnoreCreate }}
    {{- if $Field.Enum}}
      {{- if $Field.Array}}
        for i, v := range input.{{$Field.GoName}} {
          if !{{(PackageName "enum" $Model.Singular)}}.Valid{{$Field.GoName}}[v] {
            return nil, fmt.Errorf("{{$Model.Singular}}APICreate: value for member %d of field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", i, {{(PackageName "enum" $Model.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
          }
        }
      {{- else}}
        if !{{(PackageName "enum" $Model.Singular)}}.Valid{{$Field.GoName}}[input.{{$Field.GoName}}] {
          return nil, fmt.Errorf("{{$Model.Singular}}APICreate: value for field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", {{(PackageName "enum" $Model.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
        }
      {{- end}}
    {{- end}}

    ic[{{(PackageName "schema" $Model.Singular)}}.Column{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(input.{{$Field.GoName}}){{else}}input.{{$Field.GoName}}{{end}})

    {{- if $Model.HasAudit}}
      fields["{{$Field.GoName}}"] = []interface{}{input.{{$Field.GoName}}}
    {{- end}}
  {{- else if not (or $Field.IgnoreCreate $Field.IsNull) }}
    {{- if $Field.Array}}
      empty{{$Field.GoName}} := make({{$Field.GoType}}, 0)
    {{- else}}
      var empty{{$Field.GoName}} {{$Field.GoType}}
    {{- end}}

    ic[{{(PackageName "schema" $Model.Singular)}}.Column{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(empty{{$Field.GoName}}){{else}}empty{{$Field.GoName}}{{end}})
  {{- end}}
{{- end}}

  qb := sqlbuilder.Insert().Table({{(PackageName "schema" $Model.Singular)}}.Table).Columns(ic)

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APICreate: couldn't generate query: %w", err)
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APICreate: couldn't perform query: %w", err)
  }

{{if $Model.HasVersion}}
  if _, err := tx.ExecContext(ctx, "select pg_notify('model_changes', $1)", fmt.Sprintf("{{$Model.Singular}}/%s/%d", input.ID, input.Version)); err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APICreate: couldn't send postgres notification: %w", err)
  }
{{end}}

  v, err := {{$Model.Singular}}APIGet(ctx, tx, input.ID, &uid, &euid)
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APICreate: couldn't get object after creation: %w", err)
  }

  changeregistry.Add(ctx, "{{$Model.Singular}}", input.ID)

{{if $Model.HasAudit}}
  if err := modelutil.RecordAuditEvent(ctx, tx, uuid.Must(uuid.NewV4()), time.Now(), uid, euid, "create", "{{$Model.Singular}}", input.ID, fields); err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APICreate: couldn't create audit record: %w", err)
  }
{{end}}

  if queue != nil {
    if err := queue.Run(ctx, tx); err != nil {
      return nil, fmt.Errorf("{{$Model.Singular}}APICreate: couldn't run callback queue: %w", err)
    }

    vv, err := {{$Model.Singular}}APIGet(ctx, tx, input.ID, &uid, &euid)
    if err != nil {
      return nil, fmt.Errorf("{{$Model.Singular}}APICreate: couldn't get object after running callback queue: %w", err)
    }

    v = vv
  }

  return v, nil
}

func {{$Model.Singular}}APIHandleCreate(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid uuid.UUID) {
  var input {{$Model.Singular}}

  switch r.Header.Get("content-type") {
  default:
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  }

  ctx := modelutil.WithPathEntry(r.Context(), fmt.Sprintf("HTTP#{{$Model.Singular}}Create#{{FormatTemplate $Model.IDField.GoType}}", input.ID))

  tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
  if err != nil {
    panic(err)
  }
  defer tx.Rollback()

  if _, err := tx.ExecContext(ctx, "set constraints all deferred"); err != nil {
    panic(err)
  }

  options, err := modelutil.APIOptionsFromRequest(r)
  if err != nil {
    panic(err)
  }

  v, err := {{$Model.Singular}}APICreate(ctx, mctx, tx, uid, euid, time.Now(), &input, options)
  if err != nil {
    panic(err)
  }

  var result struct {
    Time time.Time "json:\"time\""
    Record *{{$Model.Singular}} "json:\"record\""
    Changed map[string][]interface{} "json:\"changed\""
  }

  result.Time = time.Now()
  result.Record = v
  result.Changed = make(map[string][]interface{})

  for k, l := range changeregistry.ChangesFromRequest(r) {
    for _, id := range l {
      v, err := modelutil.Find(ctx, k, tx, id, &uid, &euid)
      if err != nil {
        panic(err)
      }

      if v != nil {
        result.Changed[k] = append(result.Changed[k], v)
        changeregistry.RemoveFromRequest(r, k, id)
      }
    }
  }

  if err := tx.Commit(); err != nil {
    panic(err)
  }

  rw.Header().Set("content-type", "application/json")
  rw.WriteHeader(http.StatusOK)

  enc := json.NewEncoder(rw)
  if r.URL.Query().Get("_pretty") != "" {
    enc.SetIndent("", "  ")
  }

  if err := enc.Encode(result); err != nil {
    panic(err)
  }
}

func {{$Model.Singular}}APIHandleCreateMultiple(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid uuid.UUID) {
  var input struct { Records []{{$Model.Singular}} "json:\"records\"" }
  var output struct {
    Time time.Time "json:\"time\""
    Records []{{$Model.Singular}} "json:\"records\""
    Changed map[string][]interface{} "json:\"changed\""
  }

  switch r.Header.Get("content-type") {
  default:
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  }

  ctx := modelutil.WithPathEntry(r.Context(), "HTTP#{{$Model.Singular}}CreateMultiple")

  tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
  if err != nil {
    panic(err)
  }
  defer tx.Rollback()

  if _, err := tx.ExecContext(ctx, "set constraints all deferred"); err != nil {
    panic(err)
  }

  options, err := modelutil.APIOptionsFromRequest(r)
  if err != nil {
    panic(err)
  }

  for i := range input.Records {
    v, err := {{$Model.Singular}}APICreate(ctx, mctx, tx, uid, euid, time.Now(), &input.Records[i], options)
    if err != nil {
      panic(err)
    }

    output.Records = append(output.Records, *v)
  }

  output.Time = time.Now()
  output.Changed = make(map[string][]interface{})

  for k, l := range changeregistry.ChangesFromRequest(r) {
    for _, id := range l {
      v, err := modelutil.Find(ctx, k, tx, id, &uid, &euid)
      if err != nil {
        panic(err)
      }

      if v != nil {
        output.Changed[k] = append(output.Changed[k], v)
        changeregistry.RemoveFromRequest(r, k, id)
      }
    }
  }

  if err := tx.Commit(); err != nil {
    panic(err)
  }

  rw.Header().Set("content-type", "application/json")
  rw.WriteHeader(http.StatusOK)

  enc := json.NewEncoder(rw)
  if r.URL.Query().Get("_pretty") != "" {
    enc.SetIndent("", "  ")
  }

  if err := enc.Encode(output); err != nil {
    panic(err)
  }
}
{{end}}

{{if $Model.HasAPIUpdate}}
func (jsctx *JSContext) {{$Model.Singular}}Save(input *{{$Model.Singular}}) *{{$Model.Singular}} {
  v, err := {{$Model.Singular}}APISave(modelutil.WithPathEntry(jsctx.ctx, fmt.Sprintf("JS#{{$Model.Singular}}Save#{{FormatTemplate $Model.IDField.GoType}}", input.ID)), jsctx.mctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), input, nil)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (jsctx *JSContext) {{$Model.Singular}}SaveWithOptions(input *{{$Model.Singular}}, options *modelutil.APIOptions) *{{$Model.Singular}} {
  v, err := {{$Model.Singular}}APISave(modelutil.WithPathEntry(jsctx.ctx, fmt.Sprintf("JS#{{$Model.Singular}}SaveWithOptions#{{FormatTemplate $Model.IDField.GoType}}", input.ID)), jsctx.mctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), input, options)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (v *{{$Model.Singular}}) APISave(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, uid, euid uuid.UUID, now time.Time, options *modelutil.APIOptions) error {
  vv, err := {{$Model.Singular}}APISave(ctx, mctx, tx, uid, euid, now, v, options)
  if err != nil {
    return fmt.Errorf("{{$Model.Singular}}.APISave: %w", err)
  } else if vv == nil {
    return fmt.Errorf("{{$Model.Singular}}.APISave: {{$Model.Singular}}APISave did not return a valid record")
  }

  *v = *vv

  return nil
}

func {{$Model.Singular}}APISave(
  ctx context.Context,
  mctx *modelutil.ModelContext,
  tx *sql.Tx,
  uid, euid uuid.UUID,
  now time.Time,
  input *{{$Model.Singular}},
  options *modelutil.APIOptions,
) (*{{$Model.Singular}}, error) {
  if input.ID == {{if (EqualStrings $Model.IDField.GoType "int")}}0{{else}}uuid.Nil{{end}} {
    return nil, fmt.Errorf("{{$Model.Singular}}APISave: ID field was empty")
  }

  ctx, queue := modelutil.WithDeferredCallbackQueue(ctx)
  ctx, log := modelutil.WithCallbackHistoryLog(ctx)
  ctx = modelutil.WithPathEntry(ctx, fmt.Sprintf("API#{{$Model.Singular}}Save#{{FormatTemplate $Model.IDField.GoType}}", input.ID))

  p, err := {{$Model.Singular}}APIGet(ctx, tx, input.ID, &uid, &euid)
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APISave: couldn't fetch previous state: %w", err)
  }

{{if $Model.HasVersion}}
  if input.Version != p.Version {
    return nil, fmt.Errorf("{{$Model.Singular}}APISave: Version from input did not match current state (input=%d current=%d): %w", input.Version, p.Version, ErrVersionMismatch)
  }
{{end}}

{{range $Field := $Model.Fields}}
{{- if $Field.IgnoreUpdate }}
  input.{{$Field.GoName}} = p.{{$Field.GoName}}
{{- end}}
{{- if $Field.Enum}}
{{- if $Field.Array}}
  for i, v := range input.{{$Field.GoName}} {
    if !{{(PackageName "enum" $Model.Singular)}}.Valid{{$Field.GoName}}[v] {
      return nil, fmt.Errorf("{{$Model.Singular}}APISave: value for member %d of field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", i, {{(PackageName "enum" $Model.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
    }
  }
{{- else}}
  if !{{(PackageName "enum" $Model.Singular)}}.Valid{{$Field.GoName}}[input.{{$Field.GoName}}] {
    return nil, fmt.Errorf("{{$Model.Singular}}APISave: value for field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", {{(PackageName "enum" $Model.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
  }
{{- end}}
{{- end}}
{{- end}}

  exitActivity := traceregistry.Enter(ctx, &traceregistry.EventModelActivity{
    ID: uuid.Must(uuid.NewV4()),
    Time: time.Now(),
    Action: "save",
    ModelType: "{{$Model.Singular}}",
    ModelID: input.ID,
    ModelData: input,
    Path: modelutil.GetPath(ctx),
  })
  defer func() { exitActivity() }()

  b := *p

  n := 0

  forcing := false
  if options != nil {
    for _, e := range mctx.GetHandlers() {
      h, ok := e.({{$Model.Singular}}BeforeSaveHandler)
      if !ok {
        continue
      }

      if log != nil && log.Has("{{$Model.Singular}}", h.GetName(), input.ID) {
        continue
      }

      if options.ForceCallbacks.Match("{{$Model.Singular}}", h.GetName(), input.ID) {
        forcing = true
      }
    }
  }

  for {
    m := {{$Model.Singular}}FieldMaskFrom(&b, input)
    if m == ({{$Model.Singular}}FieldMask{}) && !(n == 0 && forcing) {
      break
    }
    c := b
    b = *input

    n++
    if n > 100 {
      return nil, fmt.Errorf("{{$Model.Singular}}APISave: BeforeSave callback for %s exceeded execution limit of 100 iterations", input.ID)
    }

    exitIteration := traceregistry.Enter(ctx, &traceregistry.EventIteration{
      ID: uuid.Must(uuid.NewV4()),
      Time: time.Now(),
      ObjectType: "{{$Model.Singular}}",
      ObjectID: input.ID,
      Number: n,
    })
    defer func() { exitIteration() }()

    for _, e := range mctx.GetHandlers() {
      h, ok := e.({{$Model.Singular}}BeforeSaveHandler)
      if !ok {
        continue
      }

      skipped := false
      forced := false

      if options != nil {
        if options.SkipCallbacks.MatchConsume("{{$Model.Singular}}", h.GetName(), input.ID) {
          skipped = true
        }
        if options.ForceCallbacks.MatchConsume("{{$Model.Singular}}", h.GetName(), input.ID) {
          forced = true
        }
      }

      triggered := m
      if h.Trigger != nil {
        triggered = h.Trigger.Intersect(m)
      }

      if triggered == ({{$Model.Singular}}FieldMask{}) && !forced {
        continue
      }

      before := time.Now()

      exitCallback := traceregistry.Enter(ctx, &traceregistry.EventCallback{
        ID: uuid.Must(uuid.NewV4()),
        Time: before,
        Name: h.GetQualifiedName(),
        Skipped: skipped,
        Forced: forced,
        Triggered: triggered.Changes(&c, input),
      })
      defer func() { exitCallback() }()

      a := *input

      if !skipped || forced {
        if log != nil {
          log.Add("{{$Model.Singular}}", h.GetName(), input.ID)
        }

        if err := h.Func(modelutil.WithPathEntry(ctx, fmt.Sprintf("CB#"+h.GetQualifiedName()+"#{{FormatTemplate $Model.IDField.GoType}}", input.ID)), tx, uid, euid, options, &c, input); err != nil {
          return nil, fmt.Errorf("{{$Model.Singular}}APISave: BeforeSave callback %s for %s failed: %w", h.Name, input.ID, err)
        }
      }

      traceregistry.Add(ctx, traceregistry.EventCallbackComplete{
        ID: uuid.Must(uuid.NewV4()),
        Time: time.Now(),
        Name: h.GetQualifiedName(),
        Duration: time.Now().Sub(before),
        Changed: {{$Model.Singular}}FieldMaskFrom(&a, input).Changes(&a, input),
      })

      exitCallback()
    }

    exitIteration()
  }

  uc := sqlbuilder.UpdateColumns{}

{{if $Model.HasAudit}}
  changed := make(map[string][]interface{})
{{- end}}

  skip := true

{{range $Field := $Model.Fields}}
{{- if not $Field.IgnoreUpdate}}
  if {{ (NotEqual (Join "input." $Field.GoName) $Field.GoType (Join "p." $Field.GoName) $Field.GoType) }} {
    skip = false

{{- if $Field.Enum}}
{{- if $Field.Array}}
    for i, v := range input.{{$Field.GoName}} {
      if !{{(PackageName "enum" $Model.Singular)}}.Valid{{$Field.GoName}}[v] {
        return nil, fmt.Errorf("{{$Model.Singular}}APISave: value for member %d of field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", i, {{(PackageName "enum" $Model.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
      }
    }
{{- else}}
    if !{{(PackageName "enum" $Model.Singular)}}.Valid{{$Field.GoName}}[input.{{$Field.GoName}}] {
      return nil, fmt.Errorf("{{$Model.Singular}}APISave: value for field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", {{(PackageName "enum" $Model.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
    }
{{- end}}
{{- end}}

    uc[{{(PackageName "schema" $Model.Singular)}}.Column{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(input.{{$Field.GoName}}){{else}}input.{{$Field.GoName}}{{end}})
{{- if $Model.HasAudit}}
    changed["{{$Field.GoName}}"] = []interface{}{p.{{$Field.GoName}}, input.{{$Field.GoName}}}
{{- end}}
  }
{{- end}}
{{- end}}

  if skip == false {
{{- if $Model.HasVersion}}
    input.Version = input.Version + 1
    uc[{{(PackageName "schema" $Model.Singular)}}.ColumnVersion] = sqlbuilder.Bind(input.Version)
{{- if $Model.HasAudit}}
    if input.Version != p.Version {
      changed["Version"] = []interface{}{p.Version, input.Version}
    }
{{- end}}
{{- end}}
{{- if $Model.HasUpdatedAt}}
    input.UpdatedAt = now
    uc[{{(PackageName "schema" $Model.Singular)}}.ColumnUpdatedAt] = sqlbuilder.Bind(input.UpdatedAt)
{{- if $Model.HasAudit}}
    if !input.UpdatedAt.Equal(p.UpdatedAt) {
      changed["UpdatedAt"] = []interface{}{p.UpdatedAt, input.UpdatedAt}
    }
{{- end}}
{{- end}}
{{- if $Model.HasUpdaterID}}
    input.UpdaterID = euid
    uc[{{(PackageName "schema" $Model.Singular)}}.ColumnUpdaterID] = sqlbuilder.Bind(input.UpdaterID)
{{- if $Model.HasAudit}}
    if input.UpdaterID != p.UpdaterID {
      changed["UpdaterID"] = []interface{}{p.UpdaterID, input.UpdaterID}
    }
{{- end}}
{{- end}}

    qb := sqlbuilder.Update().Table({{(PackageName "schema" $Model.Singular)}}.Table).Set(uc).Where(sqlbuilder.Eq({{(PackageName "schema" $Model.Singular)}}.ColumnID, sqlbuilder.Bind(input.ID)))

    qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
    if err != nil {
      return nil, fmt.Errorf("{{$Model.Singular}}APISave: couldn't generate query: %w", err)
    }

    if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
      return nil, fmt.Errorf("{{$Model.Singular}}APISave: couldn't update record: %w", err)
    }

{{if $Model.HasVersion}}
    if _, err := tx.ExecContext(ctx, "select pg_notify('model_changes', $1)", fmt.Sprintf("{{$Model.Singular}}/%s/%d", input.ID, input.Version)); err != nil {
      return nil, fmt.Errorf("{{$Model.Singular}}APISave: couldn't send postgres notification: %w", err)
    }
{{end}}

    changeregistry.Add(ctx, "{{$Model.Singular}}", input.ID)

{{if $Model.HasAudit}}
    if err := modelutil.RecordAuditEvent(ctx, tx, uuid.Must(uuid.NewV4()), time.Now(), uid, euid, "update", "{{$Model.Singular}}", input.ID, changed); err != nil {
      return nil, fmt.Errorf("{{$Model.Singular}}APISave: couldn't create audit record: %w", err)
    }
{{end}}
  }

  if queue != nil {
    if err := queue.Run(ctx, tx); err != nil {
      return nil, fmt.Errorf("{{$Model.Singular}}APISave: couldn't run callback queue: %w", err)
    }

    vv, err := {{$Model.Singular}}APIGet(ctx, tx, input.ID, &uid, &euid)
    if err != nil {
      return nil, fmt.Errorf("{{$Model.Singular}}APISave: couldn't get object after running callback queue: %w", err)
    }

    input = vv
  }

  return input, nil
}

func {{$Model.Singular}}APIHandleSave(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid uuid.UUID) {
  var input {{$Model.Singular}}

  switch r.Header.Get("content-type") {
  default:
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  }

  ctx := modelutil.WithPathEntry(r.Context(), fmt.Sprintf("HTTP#{{$Model.Singular}}Save#{{FormatTemplate $Model.IDField.GoType}}", input.ID))

  tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
  if err != nil {
    panic(err)
  }
  defer tx.Rollback()

  if _, err := tx.ExecContext(ctx, "set constraints all deferred"); err != nil {
    panic(err)
  }

  options, err := modelutil.APIOptionsFromRequest(r)
  if err != nil {
    panic(err)
  }

  v, err := {{$Model.Singular}}APISave(ctx, mctx, tx, uid, euid, time.Now(), &input, options)
  if err != nil {
    panic(err)
  }

  var result struct {
    Time time.Time "json:\"time\""
    Record *{{$Model.Singular}} "json:\"record\""
    Changed map[string][]interface{} "json:\"changed\""
  }

  result.Time = time.Now()
  result.Record = v
  result.Changed = make(map[string][]interface{})

  for k, l := range changeregistry.ChangesFromRequest(r) {
    for _, id := range l {
      v, err := modelutil.Find(ctx, k, tx, id, &uid, &euid)
      if err != nil {
        panic(err)
      }

      if v != nil {
        result.Changed[k] = append(result.Changed[k], v)
        changeregistry.RemoveFromRequest(r, k, id)
      }
    }
  }

  if err := tx.Commit(); err != nil {
    panic(err)
  }

  rw.Header().Set("content-type", "application/json")
  rw.WriteHeader(http.StatusOK)

  enc := json.NewEncoder(rw)
  if r.URL.Query().Get("_pretty") != "" {
    enc.SetIndent("", "  ")
  }

  if err := enc.Encode(result); err != nil {
    panic(err)
  }
}

func {{$Model.Singular}}APIHandleSaveMultiple(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid uuid.UUID) {
  var input struct { Records []{{$Model.Singular}} "json:\"records\"" }
  var output struct {
    Time time.Time "json:\"time\""
    Records []{{$Model.Singular}} "json:\"records\""
    Changed map[string][]interface{} "json:\"changed\""
  }

  switch r.Header.Get("content-type") {
  default:
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  }

  ctx := modelutil.WithPathEntry(r.Context(), "HTTP#{{$Model.Singular}}SaveMultiple")

  tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
  if err != nil {
    panic(err)
  }
  defer tx.Rollback()

  if _, err := tx.ExecContext(ctx, "set constraints all deferred"); err != nil {
    panic(err)
  }

  options, err := modelutil.APIOptionsFromRequest(r)
  if err != nil {
    panic(err)
  }

  for i := range input.Records {
    v, err := {{$Model.Singular}}APISave(ctx, mctx, tx, uid, euid, time.Now(), &input.Records[i], options)
    if err != nil {
      panic(err)
    }

    output.Records = append(output.Records, *v)
  }

  output.Time = time.Now()
  output.Changed = make(map[string][]interface{})

  for k, l := range changeregistry.ChangesFromRequest(r) {
    for _, id := range l {
      v, err := modelutil.Find(ctx, k, tx, id, &uid, &euid)
      if err != nil {
        panic(err)
      }

      if v != nil {
        output.Changed[k] = append(output.Changed[k], v)
        changeregistry.RemoveFromRequest(r, k, id)
      }
    }
  }

  if err := tx.Commit(); err != nil {
    panic(err)
  }

  rw.Header().Set("content-type", "application/json")
  rw.WriteHeader(http.StatusOK)

  enc := json.NewEncoder(rw)
  if r.URL.Query().Get("_pretty") != "" {
    enc.SetIndent("", "  ")
  }

  if err := enc.Encode(output); err != nil {
    panic(err)
  }
}

func {{$Model.Singular}}APIFindAndModify(
  ctx context.Context,
  mctx *modelutil.ModelContext,
  tx *sql.Tx,
  uid, euid uuid.UUID,
  now time.Time,
  id {{$Model.IDField.GoType}},
  options *modelutil.APIOptions,
  modify func(v *{{$Model.Singular}}) error,
) (*{{$Model.Singular}}, error) {
  v, err := {{$Model.Singular}}APIGet(ctx, tx, id, &uid, &euid)
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APIFindAndModify: error fetching record {{$Model.IDField.FormatType}}: %w", id, err)
  } else if v == nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APIFindAndModify: could not find record {{$Model.IDField.FormatType}}", id)
  }

  if err := modify(v); err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APIFindAndModify: error modifying record {{$Model.IDField.FormatType}}: %w", id, err)
  }

  vv, err := {{$Model.Singular}}APISave(ctx, mctx, tx, uid, euid, now, v, options)
  if err != nil {
    return nil, fmt.Errorf("{{$Model.Singular}}APIFindAndModify: error saving record {{$Model.IDField.FormatType}}: %w", id, err)
  }

  return vv, nil
}

func (v *{{$Model.Singular}}) APIFindAndModify(
  ctx context.Context,
  mctx *modelutil.ModelContext,
  tx *sql.Tx,
  uid, euid uuid.UUID,
  now time.Time,
  options *modelutil.APIOptions,
  modify func(v *{{$Model.Singular}}) error,
) error {
  vv, err := {{$Model.Singular}}APIFindAndModify(ctx, mctx, tx, uid, euid, now, v.ID, options, modify)
  if err != nil {
    return fmt.Errorf("{{$Model.Singular}}.APIFindAndModify: %w", err)
  }

  *v = *vv

  return nil
}

func {{$Model.Singular}}APIFindAndModifyOutsideTransaction(
  ctx context.Context,
  mctx *modelutil.ModelContext,
  db *sql.DB,
  uid, euid uuid.UUID,
  now time.Time,
  id {{$Model.IDField.GoType}},
  options *modelutil.APIOptions,
  modify func(v *{{$Model.Singular}}) error,
) (*{{$Model.Singular}}, error) {
  return retrydb.LinearBackoff2(ctx, db, &sql.TxOptions{Isolation: sql.LevelSerializable}, 5, time.Millisecond*500, func(ctx context.Context, tx *sql.Tx) (*{{$Model.Singular}}, error) {
    v, err := {{$Model.Singular}}APIFindAndModify(ctx, mctx, tx, uid, euid, now, id, options, modify)
    if err != nil {
      return v, fmt.Errorf("{{$Model.Singular}}APIFindAndModifyOutsideTransaction: %w", err)
    }
    return v, nil
  })
}

func (v *{{$Model.Singular}}) APIFindAndModifyOutsideTransaction(
  ctx context.Context,
  mctx *modelutil.ModelContext,
  db *sql.DB,
  uid, euid uuid.UUID,
  now time.Time,
  options *modelutil.APIOptions,
  modify func(v *{{$Model.Singular}}) error,
) error {
  vv, err := {{$Model.Singular}}APIFindAndModifyOutsideTransaction(ctx, mctx, db, uid, euid, now, v.ID, options, modify)
  if err != nil {
    return fmt.Errorf("{{$Model.Singular}}.APIFindAndModifyOutsideTransaction: %w", err)
  }

  *v = *vv

  return nil
}
{{end}}

{{if $Model.HasCreatedAt}}
func (jsctx *JSContext) {{$Model.Singular}}ChangeCreatedAt(id {{$Model.IDField.GoType}}, createdAt time.Time) {
  if err := {{$Model.Singular}}APIChangeCreatedAt(jsctx.ctx, jsctx.mctx, jsctx.tx, id, createdAt); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}

func {{$Model.Singular}}APIChangeCreatedAt(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, id {{$Model.IDField.GoType}}, createdAt time.Time) error {
  if id == {{if (EqualStrings $Model.IDField.GoType "int")}}0{{else}}uuid.Nil{{end}} {
    return fmt.Errorf("{{$Model.Singular}}APIChangeCreatedAt: id was empty")
  }
  if createdAt.IsZero() {
    return fmt.Errorf("{{$Model.Singular}}APIChangeCreatedAt: createdAt was empty")
  }

  qb := sqlbuilder.Update().Table({{(PackageName "schema" $Model.Singular)}}.Table).Set(sqlbuilder.UpdateColumns{
    {{(PackageName "schema" $Model.Singular)}}.ColumnCreatedAt: sqlbuilder.Bind(createdAt),
  }).Where(sqlbuilder.Eq({{(PackageName "schema" $Model.Singular)}}.ColumnID, sqlbuilder.Bind(id)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return fmt.Errorf("{{$Model.Singular}}APIChangeCreatedAt: couldn't generate query: %w", err)
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return fmt.Errorf("{{$Model.Singular}}APIChangeCreatedAt: couldn't update record: %w", err)
  }

  return nil
}
{{end}}

{{if $Model.HasCreatorID}}
func (jsctx *JSContext) {{$Model.Singular}}ChangeCreatorID(id, creatorID uuid.UUID) {
  if err := {{$Model.Singular}}APIChangeCreatorID(jsctx.ctx, jsctx.mctx, jsctx.tx, id, creatorID); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}

func {{$Model.Singular}}APIChangeCreatorID(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, id, creatorID uuid.UUID) error {
  if id == {{if (EqualStrings $Model.IDField.GoType "int")}}0{{else}}uuid.Nil{{end}} {
    return fmt.Errorf("{{$Model.Singular}}APIChangeCreatorID: id was empty")
  }
  if creatorID == uuid.Nil {
    return fmt.Errorf("{{$Model.Singular}}APIChangeCreatorID: creatorID was empty")
  }

  qb := sqlbuilder.Update().Table({{(PackageName "schema" $Model.Singular)}}.Table).Set(sqlbuilder.UpdateColumns{
    {{(PackageName "schema" $Model.Singular)}}.ColumnCreatorID: sqlbuilder.Bind(creatorID),
  }).Where(sqlbuilder.Eq({{(PackageName "schema" $Model.Singular)}}.ColumnID, sqlbuilder.Bind(id)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return fmt.Errorf("{{$Model.Singular}}APIChangeCreatorID: couldn't generate query: %w", err)
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return fmt.Errorf("{{$Model.Singular}}APIChangeCreatorID: couldn't update record: %w", err)
  }

  return nil
}
{{end}}

{{if $Model.HasUpdatedAt}}
func (jsctx *JSContext) {{$Model.Singular}}ChangeUpdatedAt(id {{$Model.IDField.GoType}}, updatedAt time.Time) {
  if err := {{$Model.Singular}}APIChangeUpdatedAt(jsctx.ctx, jsctx.mctx, jsctx.tx, id, updatedAt); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}

func {{$Model.Singular}}APIChangeUpdatedAt(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, id {{$Model.IDField.GoType}}, updatedAt time.Time) error {
  if id == {{if (EqualStrings $Model.IDField.GoType "int")}}0{{else}}uuid.Nil{{end}} {
    return fmt.Errorf("{{$Model.Singular}}APIChangeUpdatedAt: id was empty")
  }
  if updatedAt.IsZero() {
    return fmt.Errorf("{{$Model.Singular}}APIChangeUpdatedAt: updatedAt was empty")
  }

  qb := sqlbuilder.Update().Table({{(PackageName "schema" $Model.Singular)}}.Table).Set(sqlbuilder.UpdateColumns{
    {{(PackageName "schema" $Model.Singular)}}.ColumnUpdatedAt: sqlbuilder.Bind(updatedAt),
  }).Where(sqlbuilder.Eq({{(PackageName "schema" $Model.Singular)}}.ColumnID, sqlbuilder.Bind(id)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return fmt.Errorf("{{$Model.Singular}}APIChangeUpdatedAt: couldn't generate query: %w", err)
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return fmt.Errorf("{{$Model.Singular}}APIChangeUpdatedAt: couldn't update record: %w", err)
  }

  return nil
}
{{end}}

{{if $Model.HasUpdaterID}}
func (jsctx *JSContext) {{$Model.Singular}}ChangeUpdaterID(id, updaterID uuid.UUID) {
  if err := {{$Model.Singular}}APIChangeUpdaterID(jsctx.ctx, jsctx.mctx, jsctx.tx, id, updaterID); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}

func {{$Model.Singular}}APIChangeUpdaterID(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, id, updaterID uuid.UUID) error {
  if id == {{if (EqualStrings $Model.IDField.GoType "int")}}0{{else}}uuid.Nil{{end}} {
    return fmt.Errorf("{{$Model.Singular}}APIChangeUpdaterID: id was empty")
  }
  if updaterID == uuid.Nil {
    return fmt.Errorf("{{$Model.Singular}}APIChangeUpdaterID: updaterID was empty")
  }

  qb := sqlbuilder.Update().Table({{(PackageName "schema" $Model.Singular)}}.Table).Set(sqlbuilder.UpdateColumns{
    {{(PackageName "schema" $Model.Singular)}}.ColumnUpdaterID: sqlbuilder.Bind(updaterID),
  }).Where(sqlbuilder.Eq({{(PackageName "schema" $Model.Singular)}}.ColumnID, sqlbuilder.Bind(id)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return fmt.Errorf("{{$Model.Singular}}APIChangeUpdaterID: couldn't generate query: %w", err)
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return fmt.Errorf("{{$Model.Singular}}APIChangeUpdaterID: couldn't update record: %w", err)
  }

  return nil
}
{{end}}

{{range $Field := $Model.Fields}}
{{- if not (eq $Field.Sequence "")}}
func (jsctx *JSContext) {{$Model.Singular}}Set{{$Field.GoName}}IfEmpty(v *{{$Model.Singular}}) {
  if err := {{$Model.Singular}}APISet{{$Field.GoName}}IfEmpty(jsctx.ctx, jsctx.tx, v); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}

func {{$Model.Singular}}APISet{{$Field.GoName}}IfEmpty(ctx context.Context, tx *sql.Tx, v *{{$Model.Singular}}) error {
  if v.{{$Field.GoName}} != 0 {
    return nil
  }

  if err := tx.QueryRowContext(ctx, "select nextval('{{$Field.Sequence}}')").Scan(&v.{{$Field.GoName}}); err != nil {
    return fmt.Errorf("{{$Model.Singular}}APISet{{$Field.GoName}}IfEmpty: couldn't get sequence value for field \"{{$Field.APIName}}\" from sequence \"{{$Field.Sequence}}\": %w", err)
  }

  return nil
}
{{- end}}
{{end}}

{{range $Process := $Model.Processes}}
type {{$Model.Singular}}Process{{$Process}} struct { Value *{{$Model.Singular}} }

func (v *{{$Model.Singular}}) ProcessFor{{$Process}}() *{{$Model.Singular}}Process{{$Process}} {
  return &{{$Model.Singular}}Process{{$Process}}{Value: v}
}

func (p *{{$Model.Singular}}Process{{$Process}}) Name() string {
  return "{{$Model.Singular}}.{{$Process}}"
}
func (p *{{$Model.Singular}}Process{{$Process}}) GetStatus() string {
  return p.Value.{{$Process}}Status
}
func (p *{{$Model.Singular}}Process{{$Process}}) GetCompletedAt() *time.Time {
  return p.Value.{{$Process}}CompletedAt
}
func (p *{{$Model.Singular}}Process{{$Process}}) SetCompletedAt(completedAt *time.Time) {
  p.Value.{{$Process}}CompletedAt = completedAt
}
func (p *{{$Model.Singular}}Process{{$Process}}) GetStartedAt() *time.Time {
  return p.Value.{{$Process}}StartedAt
}
func (p *{{$Model.Singular}}Process{{$Process}}) SetStartedAt(startedAt *time.Time) {
  p.Value.{{$Process}}StartedAt = startedAt
}
func (p *{{$Model.Singular}}Process{{$Process}}) GetDeadline() *time.Time {
  return p.Value.{{$Process}}Deadline
}
func (p *{{$Model.Singular}}Process{{$Process}}) SetDeadline(deadline *time.Time) {
  p.Value.{{$Process}}Deadline = deadline
}
func (p *{{$Model.Singular}}Process{{$Process}}) GetFailureMessage() string {
  return p.Value.{{$Process}}FailureMessage
}
func (p *{{$Model.Singular}}Process{{$Process}}) SetFailureMessage(failureMessage string) {
  p.Value.{{$Process}}FailureMessage = failureMessage
}
{{end}}
`
