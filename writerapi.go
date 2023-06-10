package main

import (
	"go/types"
	"io"
	"strings"
	"text/template"
)

type APIWriter struct{ dir string }

func NewAPIWriter(dir string) *APIWriter { return &APIWriter{dir: dir} }

func (APIWriter) Name() string     { return "api" }
func (APIWriter) Language() string { return "go" }
func (w APIWriter) File(typeName string, _ *types.Named, _ *types.Struct) string {
	return w.dir + "/" + strings.ToLower(typeName) + "_api.go"
}

func (APIWriter) Imports(typeName string, _ *types.Named, _ *types.Struct) []string {
	return []string{
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
		"movingdata.com/p/wbi/models/modelapifilter/" + strings.ToLower(typeName) + "apifilter",
		"movingdata.com/p/wbi/models/modelenum/" + strings.ToLower(typeName) + "enum",
		"movingdata.com/p/wbi/models/modelschema/" + strings.ToLower(typeName) + "schema",
	}
}

func (w *APIWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	model, err := makeModel(typeName, namedType, structType)
	if err != nil {
		return err
	}
	return apiTemplate.Execute(wr, *model)
}

var apiTemplate = template.Must(template.New("apiTemplate").Funcs(tplFunc).Parse(`
{{$Type := .}}

func init() {
  modelutil.RegisterFinder("{{$Type.Singular}}", func(ctx context.Context, db modelutil.RowQueryerContext, id interface{}, uid, euid *uuid.UUID) (interface{}, error) {
    idValue, ok := id.({{$Type.IDField.GoType}})
    if !ok {
      return nil, fmt.Errorf("{{$Type.Singular}}: id should be {{$Type.IDField.GoType}}; was instead %T", id)
    }

    v, err := {{$Type.Singular}}APIGet(ctx, db, idValue, uid, euid)
    if err != nil {
      return nil, err
    }
    if v == nil {
      return nil, nil
    }
    return v, nil
  })
}

{{range $Field := $Type.Fields}}
{{- if $Field.Enum}}
func (jsctx *JSContext) {{$Type.Singular}}EnumValid{{$Field.GoName}}(v string) bool {
  return {{(PackageName "enum" $Type.Singular)}}.Valid{{$Field.GoName}}[v]
}

func (jsctx *JSContext) {{$Type.Singular}}EnumValues{{$Field.GoName}}() []string {
  return {{(PackageName "enum" $Type.Singular)}}.Values{{$Field.GoName}}
}

func (jsctx *JSContext) {{$Type.Singular}}EnumLabel{{$Field.GoName}}(v string) string {
  return {{(PackageName "enum" $Type.Singular)}}.Labels{{$Field.GoName}}[v]
}
{{- end}}
{{end}}

func (jsctx *JSContext) {{$Type.Singular}}Get(id {{$Type.IDField.GoType}}) *{{$Type.Singular}} {
  v, err := {{$Type.Singular}}APIGet(jsctx.ctx, jsctx.tx, id, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (v *{{$Type.Singular}}) APIGet(ctx context.Context, db modelutil.RowQueryerContext, id {{$Type.IDField.GoType}}, uid, euid *uuid.UUID) error {
  vv, err := {{$Type.Singular}}APIGet(ctx, db, id, uid, euid)
  if err != nil {
    return fmt.Errorf("{{$Type.Singular}}.APIGet: %w", err)
  } else if vv == nil {
    return fmt.Errorf("{{$Type.Singular}}.APIGet: could not find record {{$Type.IDField.FormatType}}", id)
  }

  *v = *vv

  return nil
}

func {{$Type.Singular}}APIGet(ctx context.Context, db modelutil.RowQueryerContext, id {{$Type.IDField.GoType}}, uid, euid *uuid.UUID) (*{{$Type.Singular}}, error) {
  qb := sqlbuilder.Select().From({{(PackageName "schema" $Type.Singular)}}.Table).Columns(modelutil.ColumnsAsExpressions({{(PackageName "schema" $Type.Singular)}}.Columns)...)

{{if $Type.HasUserFilter}}
  qb = {{$Type.Singular}}UserFilter(qb, euid)
{{- end}}

  qb = qb.AndWhere(sqlbuilder.Eq({{(PackageName "schema" $Type.Singular)}}.ColumnID, sqlbuilder.Bind(id)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APIGet: couldn't generate query: %w", err)
  }

  var v {{$Type.Singular}}
{{range $Field := $Type.Fields}}
  {{if $Field.ScanType}}var x{{$Field.GoName}} {{$Field.ScanType}}{{end}}
{{- end}}
  if err := db.QueryRowContext(ctx, qs, qv...).Scan({{range $i, $Field := $Type.Fields}}{{if $Field.ScanType}}&x{{$Field.GoName}}{{else if $Field.Array}}pq.Array(&v.{{$Field.GoName}}){{else}}&v.{{$Field.GoName}}{{end}}, {{end}}); err != nil {
    if err == sql.ErrNoRows {
      return nil, nil
    }

    return nil, fmt.Errorf("{{$Type.Singular}}APIGet: couldn't perform query: %w", err)
  }

{{range $Field := $Type.Fields}}
  {{if $Field.ScanType}}v.{{$Field.GoName}} = ({{$Field.GoType}})(x{{$Field.GoName}}){{end}}
{{- end}}

  return &v, nil
}

func {{$Type.Singular}}APIHandleGet(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid *uuid.UUID) {
  vars := mux.Vars(r)

{{if (EqualStrings $Type.IDField.GoType "int")}}
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

  v, err := {{$Type.Singular}}APIGet(r.Context(), db, id, uid, euid)
  if err != nil {
    panic(err)
  }

  if v == nil {
    http.Error(rw, fmt.Sprintf("{{$Type.Singular}} with id {{FormatTemplate $Type.IDField.GoType}} not found", id), http.StatusNotFound)
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

type {{$Type.Singular}}APISearchResponse struct {
  Records []*{{$Type.Singular}} "json:\"records\""
  Total int "json:\"total\""
  Time time.Time "json:\"time\""
}

func (jsctx *JSContext) {{$Type.Singular}}Search(p {{(PackageName "apifilter" $Type.Singular)}}.SearchParameters) *{{$Type.Singular}}APISearchResponse {
  v, err := {{$Type.Singular}}APISearch(jsctx.ctx, jsctx.tx, &p, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func {{$Type.Singular}}APISearch(ctx context.Context, db modelutil.QueryerContextAndRowQueryerContext, p *{{(PackageName "apifilter" $Type.Singular)}}.SearchParameters, uid, euid *uuid.UUID) (*{{$Type.Singular}}APISearchResponse, error) {
  qb := sqlbuilder.Select().From({{(PackageName "schema" $Type.Singular)}}.Table).Columns(modelutil.ColumnsAsExpressions({{(PackageName "schema" $Type.Singular)}}.Columns)...)

{{- if $Type.HasUserFilter}}
  qb = {{$Type.Singular}}UserFilter(qb, euid)
{{- end}}

  qb = p.AddFilters(qb)

  qb1 := p.AddLimits(qb)
  qs1, qv1, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb1.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APISearch: couldn't generate result query: %w", err)
  }

  qb2 := qb.Columns(sqlbuilder.Func("count", sqlbuilder.Literal("*")))
  qs2, qv2, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb2.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APISearch: couldn't generate summary query: %w", err)
  }

  rows, err := db.QueryContext(ctx, qs1, qv1...)
  if err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APISearch: couldn't perform result query: %w", err)
  }
  defer rows.Close()

  a := make([]*{{$Type.Singular}}, 0)
  for rows.Next() {
    var m {{$Type.Singular}}
{{range $Field := $Type.Fields}}
    {{if $Field.ScanType}}var x{{$Field.GoName}} {{$Field.ScanType}}{{end}}
{{- end}}
    if err := rows.Scan({{range $i, $Field := $Type.Fields}}{{if $Field.ScanType}}&x{{$Field.GoName}}{{else if $Field.Array}}pq.Array(&m.{{$Field.GoName}}){{else}}&m.{{$Field.GoName}}{{end}} /* {{$i}} */, {{end}}); err != nil {
      return nil, fmt.Errorf("{{$Type.Singular}}APISearch: couldn't scan result row: %w", err)
    }

{{range $Field := $Type.Fields}}
    {{if $Field.ScanType}}m.{{$Field.GoName}} = ({{$Field.GoType}})(x{{$Field.GoName}}){{end}}
{{- end}}

    a = append(a, &m)
  }

  if err := rows.Close(); err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APISearch: couldn't close result row set: %w", err)
  }

  var total int
  if err := db.QueryRowContext(ctx, qs2, qv2...).Scan(&total); err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APISearch: couldn't perform summary query: %w", err)
  }

  return &{{$Type.Singular}}APISearchResponse{
    Records: a,
    Total: total,
    Time: time.Now(),
  }, nil
}

func (jsctx *JSContext) {{$Type.Singular}}Find(p {{(PackageName "apifilter" $Type.Singular)}}.FilterParameters) *{{$Type.Singular}} {
  v, err := {{$Type.Singular}}APIFind(jsctx.ctx, jsctx.tx, &p, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func {{$Type.Singular}}APIFind(ctx context.Context, db modelutil.QueryerContextAndRowQueryerContext, p *{{(PackageName "apifilter" $Type.Singular)}}.FilterParameters, uid, euid *uuid.UUID) (*{{$Type.Singular}}, error) {
  qb := sqlbuilder.Select().From({{(PackageName "schema" $Type.Singular)}}.Table).Columns(modelutil.ColumnsAsExpressions({{(PackageName "schema" $Type.Singular)}}.Columns)...)

{{- if $Type.HasUserFilter}}
  qb = {{$Type.Singular}}UserFilter(qb, euid)
{{- end}}

  qb = p.AddFilters(qb)

  qs1, qv1, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APIFind: couldn't generate result query: %w", err)
  }

  qb2 := qb.Columns(sqlbuilder.Func("count", sqlbuilder.Literal("*")))
  qs2, qv2, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb2.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APIFind: couldn't generate summary query: %w", err)
  }

  var m {{$Type.Singular}}
{{range $Field := $Type.Fields}}
  {{if $Field.ScanType}}var x{{$Field.GoName}} {{$Field.ScanType}}{{end}}
{{- end}}
  if err := db.QueryRowContext(ctx, qs1, qv1...).Scan({{range $i, $Field := $Type.Fields}}{{if $Field.Array}}pq.Array(&m.{{$Field.GoName}}){{else}}&m.{{$Field.GoName}}{{end}}, {{end}}); err != nil {
    if err == sql.ErrNoRows {
      return nil, nil
    }

    return nil, fmt.Errorf("{{$Type.Singular}}APIFind: couldn't scan result row: %w", err)
  }

{{range $Field := $Type.Fields}}
  {{if $Field.ScanType}}m.{{$Field.GoName}} = ({{$Field.GoType}})(x{{$Field.GoName}}){{end}}
{{- end}}

  var total int
  if err := db.QueryRowContext(ctx, qs2, qv2...).Scan(&total); err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APIFind: couldn't perform summary query: %w", err)
  }

  if total != 1 {
    return nil, fmt.Errorf("{{$Type.Singular}}APIFind: expected one result, got %d", total)
  }

  return &m, nil
}

func {{$Type.Singular}}APIHandleSearch(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid *uuid.UUID) {
  var p {{(PackageName "apifilter" $Type.Singular)}}.SearchParameters
  if err := modelutil.DecodeStruct(r.URL.Query(), &p); err != nil {
    panic(err)
  }

  v, err := {{$Type.Singular}}APISearch(r.Context(), db, &p, uid, euid)
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

func {{$Type.Singular}}APIHandleSearchCSV(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid *uuid.UUID) {
  var p {{(PackageName "apifilter" $Type.Singular)}}.SearchParameters
  if err := modelutil.DecodeStruct(r.URL.Query(), &p); err != nil {
    panic(err)
  }

  v, err := {{$Type.Singular}}APISearch(r.Context(), db, &p, uid, euid)
  if err != nil {
    panic(err)
  }

  rw.Header().Set("content-type", "text/csv")
  rw.Header().Set("content-disposition", "attachment;filename={{$Type.Plural}} Search Results.csv")
  rw.WriteHeader(http.StatusOK)

  wr := csv.NewWriter(rw)

  if err := wr.Write([]string{ {{range $Field := $Type.Fields}}"{{$Field.GoName | UCLS}}",{{end}} }); err != nil {
    panic(err)
  }

  for _, e := range v.Records {
    if err := wr.Write([]string{ {{range $Field := $Type.Fields}}fmt.Sprintf("%v", e.{{$Field.GoName}}),{{end}} }); err != nil {
      panic(err)
    }
  }

  wr.Flush()
}

{{if (or $Type.HasAPICreate $Type.HasAPIUpdate)}}
type {{$Type.Singular}}FieldMask struct {
{{range $Field := $Type.Fields}}
  {{$Field.GoName}} bool
{{- end}}
}

func (m {{$Type.Singular}}FieldMask) ModelName() string {
  return "{{$Type.Singular}}"
}

func (m {{$Type.Singular}}FieldMask) Fields() []string {
  return modelutil.FieldMaskTrueFields("{{$Type.Singular}}", m)
}

func (m {{$Type.Singular}}FieldMask) Union(other {{$Type.Singular}}FieldMask) {{$Type.Singular}}FieldMask {
  var out {{$Type.Singular}}FieldMask
  modelutil.FieldMaskUnion(m, other, &out)
  return out
}

func (m {{$Type.Singular}}FieldMask) Intersect(other {{$Type.Singular}}FieldMask) {{$Type.Singular}}FieldMask {
  var out {{$Type.Singular}}FieldMask
  modelutil.FieldMaskIntersect(m, other, &out)
  return out
}

func (m {{$Type.Singular}}FieldMask) Match(a, b *{{$Type.Singular}}) bool {
  return modelutil.FieldMaskMatch(m, a, b)
}

func (m *{{$Type.Singular}}FieldMask) From(a, b *{{$Type.Singular}}) {
  modelutil.FieldMaskFrom(a, b, m)
}

func (m {{$Type.Singular}}FieldMask) Changes(a, b *{{$Type.Singular}}) ([]traceregistry.Change) {
  return modelutil.FieldMaskChanges(m, a, b)
}

func {{$Type.Singular}}FieldMaskFrom(a, b *{{$Type.Singular}}) {{$Type.Singular}}FieldMask {
  var m {{$Type.Singular}}FieldMask
  m.From(a, b)
  return m
}

type {{$Type.Singular}}BeforeSaveHandlerFunc func(ctx context.Context, tx *sql.Tx, uid, euid uuid.UUID, options *modelutil.APIOptions, current, proposed *{{$Type.Singular}}) error

type {{$Type.Singular}}BeforeSaveHandler struct {
  Name string
  Trigger *{{$Type.Singular}}FieldMask
  Change *{{$Type.Singular}}FieldMask
  Read []modelutil.FieldMask
  Write []modelutil.FieldMask
  DeferredRead []modelutil.FieldMask
  DeferredWrite []modelutil.FieldMask
  Func {{$Type.Singular}}BeforeSaveHandlerFunc
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetName() string {
  return h.Name
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetModelName() string {
  return "{{$Type.Singular}}"
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetQualifiedName() string {
  return "{{$Type.Singular}}." + h.GetName()
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetTriggers() []string {
  if h.Trigger != nil {
    return h.Trigger.Fields()
  }

  return []string{ {{range $Field := $Type.Fields}}"{{$Type.Singular}}.{{$Field.GoName}}",{{end}} }
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetTriggerMask() modelutil.FieldMask {
  return h.Trigger
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetChanges() []string {
  if h.Change != nil {
    return h.Change.Fields()
  }

  return []string{ {{range $Field := $Type.Fields}}"{{$Type.Singular}}.{{$Field.GoName}}",{{end}} }
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetChangeMask() modelutil.FieldMask {
  return h.Change
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetReads() []string {
  var a []string

  for _, e := range h.Read {
    a = append(a, e.Fields()...)
  }

  return a
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetReadMasks() []modelutil.FieldMask {
  return h.Read
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetWrites() []string {
  var a []string

  for _, e := range h.Write {
    a = append(a, e.Fields()...)
  }

  return a
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetWriteMasks() []modelutil.FieldMask {
  return h.Write
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetDeferredReads() []string {
  var a []string

  for _, e := range h.DeferredRead {
    a = append(a, e.Fields()...)
  }

  return a
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetDeferredReadMasks() []modelutil.FieldMask {
  return h.DeferredRead
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetDeferredWrites() []string {
  var a []string

  for _, e := range h.DeferredWrite {
    a = append(a, e.Fields()...)
  }

  return a
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetDeferredWriteMasks() []modelutil.FieldMask {
  return h.DeferredWrite
}

func (h *{{$Type.Singular}}BeforeSaveHandler) Match(a, b *{{$Type.Singular}}) bool {
  if h.Trigger == nil {
    return true
  }

  return h.Trigger.Match(a, b)
}
{{end}}

{{if $Type.HasAPICreate}}
func (jsctx *JSContext) {{$Type.Singular}}Create(input {{$Type.Singular}}) *{{$Type.Singular}} {
  v, err := {{$Type.Singular}}APICreate(modelutil.WithPathEntry(jsctx.ctx, fmt.Sprintf("JS#{{$Type.Singular}}Create#{{FormatTemplate $Type.IDField.GoType}}", input.ID)), jsctx.mctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), &input, nil)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (jsctx *JSContext) {{$Type.Singular}}CreateWithOptions(input {{$Type.Singular}}, options modelutil.APIOptions) *{{$Type.Singular}} {
  v, err := {{$Type.Singular}}APICreate(modelutil.WithPathEntry(jsctx.ctx, fmt.Sprintf("JS#{{$Type.Singular}}CreateWithOptions#{{FormatTemplate $Type.IDField.GoType}}", input.ID)), jsctx.mctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), &input, &options)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (v *{{$Type.Singular}}) APICreate(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, uid, euid uuid.UUID, now time.Time, options *modelutil.APIOptions) error {
  vv, err := {{$Type.Singular}}APICreate(ctx, mctx, tx, uid, euid, now, v, options)
  if err != nil {
    return fmt.Errorf("{{$Type.Singular}}.APICreate: %w", err)
  } else if vv == nil {
    return fmt.Errorf("{{$Type.Singular}}.APICreate: {{$Type.Singular}}APICreate did not return a valid record")
  }

  *v = *vv

  return nil
}

func {{$Type.Singular}}APICreate(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, uid, euid uuid.UUID, now time.Time, input *{{$Type.Singular}}, options *modelutil.APIOptions) (*{{$Type.Singular}}, error) {
  if input.ID == {{if (EqualStrings $Type.IDField.GoType "int")}}0{{else}}uuid.Nil{{end}} {
    return nil, fmt.Errorf("{{$Type.Singular}}APICreate: ID field was empty")
  }

  ctx, queue := modelutil.WithDeferredCallbackQueue(ctx)
  ctx, log := modelutil.WithCallbackHistoryLog(ctx)
  ctx = modelutil.WithPathEntry(ctx, fmt.Sprintf("API#{{$Type.Singular}}Create#{{FormatTemplate $Type.IDField.GoType}}", input.ID))

  ic := sqlbuilder.InsertColumns{}

{{if $Type.HasAudit}}
  fields := make(map[string][]interface{})
{{- end}}

{{range $Field := $Type.Fields}}
{{- if $Field.Enum}}
{{- if $Field.Array}}
  for i, v := range input.{{$Field.GoName}} {
    if !{{(PackageName "enum" $Type.Singular)}}.Valid{{$Field.GoName}}[v] {
      return nil, fmt.Errorf("{{$Type.Singular}}APICreate: value for member %d of field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", i, {{(PackageName "enum" $Type.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
    }
  }
{{- else}}
  if !{{(PackageName "enum" $Type.Singular)}}.Valid{{$Field.GoName}}[input.{{$Field.GoName}}] {
    return nil, fmt.Errorf("{{$Type.Singular}}APICreate: value for field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", {{(PackageName "enum" $Type.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
  }
{{- end}}
{{- end}}
{{end}}

{{range $Field := $Type.Fields}}
{{- if not (eq $Field.Sequence "")}}
  if input.{{$Field.GoName}} == 0 {
    if err := tx.QueryRowContext(ctx, "select nextval('{{$Field.Sequence}}')").Scan(&input.{{$Field.GoName}}); err != nil {
      return nil, fmt.Errorf("{{$Type.Singular}}APICreate: couldn't get sequence value for field \"{{$Field.APIName}}\" from sequence \"{{$Field.Sequence}}\": %w", err)
    }
  }
{{- end}}
{{end}}

{{range $Field := $Type.Fields}}
{{- if $Field.Array}}
  if input.{{$Field.GoName}} == nil {
    input.{{$Field.GoName}} = make({{$Field.GoType}}, 0)
  }
{{- end}}
{{end}}

{{- if $Type.HasID}}
  ic[{{(PackageName "schema" $Type.Singular)}}.ColumnID] = sqlbuilder.Bind(input.ID)
{{- if $Type.HasAudit}}
  fields["ID"] = []interface{}{input.ID}
{{- end}}
{{- end}}
{{- if $Type.HasCreatedAt}}
  input.CreatedAt = now
  ic[{{(PackageName "schema" $Type.Singular)}}.ColumnCreatedAt] = sqlbuilder.Bind(input.CreatedAt)
{{- if $Type.HasAudit}}
  fields["CreatedAt"] = []interface{}{input.CreatedAt}
{{- end}}
{{- end}}
{{- if $Type.HasUpdatedAt}}
  input.UpdatedAt = now
  ic[{{(PackageName "schema" $Type.Singular)}}.ColumnUpdatedAt] = sqlbuilder.Bind(input.UpdatedAt)
{{- if $Type.HasAudit}}
  fields["UpdatedAt"] = []interface{}{input.UpdatedAt}
{{- end}}
{{- end}}
{{- if $Type.HasCreatorID}}
  input.CreatorID = euid
  ic[{{(PackageName "schema" $Type.Singular)}}.ColumnCreatorID] = sqlbuilder.Bind(input.CreatorID)
{{- if $Type.HasAudit}}
  fields["CreatorID"] = []interface{}{input.CreatorID}
{{- end}}
{{- end}}
{{- if $Type.HasUpdaterID}}
  input.UpdaterID = euid
  ic[{{(PackageName "schema" $Type.Singular)}}.ColumnUpdaterID] = sqlbuilder.Bind(input.UpdaterID)
{{- if $Type.HasAudit}}
  fields["UpdaterID"] = []interface{}{input.UpdaterID}
{{- end}}
{{- end}}
{{- if $Type.HasVersion}}
  switch input.Version {
  case 0:
    // initialise to 1 if not supplied
    input.Version = 1
  case 1:
    // nothing
  default:
    return nil, fmt.Errorf("{{$Type.Singular}}APICreate: Version from input should be 0 or 1; was instead %d: %w", input.Version, ErrVersionMismatch)
  }
  ic[{{(PackageName "schema" $Type.Singular)}}.ColumnVersion] = sqlbuilder.Bind(input.Version)
{{- if $Type.HasAudit}}
  fields["Version"] = []interface{}{input.Version}
{{- end}}
{{- end}}

  exitActivity := traceregistry.Enter(ctx, &traceregistry.EventModelActivity{
    ID: uuid.Must(uuid.NewV4()),
    Time: time.Now(),
    Action: "create",
    ModelType: "{{$Type.Singular}}",
    ModelID: input.ID,
    ModelData: input,
    Path: modelutil.GetPath(ctx),
  })
  defer func() { exitActivity() }()

  b := {{$Type.Singular}}{}

  n := 0

  for {
    m := {{$Type.Singular}}FieldMaskFrom(&b, input)
    if m == ({{$Type.Singular}}FieldMask{}) {
      break
    }
    c := b
    b = *input

    n++
    if n > 100 {
      return nil, fmt.Errorf("{{$Type.Singular}}APICreate: BeforeSave callback for %s exceeded execution limit of 100 iterations", input.ID)
    }

    exitIteration := traceregistry.Enter(ctx, &traceregistry.EventIteration{
      ID: uuid.Must(uuid.NewV4()),
      Time: time.Now(),
      ObjectType: "{{$Type.Singular}}",
      ObjectID: input.ID,
      Number: n,
    })
    defer func() { exitIteration() }()

    for _, e := range mctx.GetHandlers() {
      h, ok := e.({{$Type.Singular}}BeforeSaveHandler)
      if !ok {
        continue
      }

      skipped := false
      forced := false

      if options != nil {
        if options.SkipCallbacks.MatchConsume("{{$Type.Singular}}", h.GetName(), input.ID) {
          skipped = true
        }
        if options.ForceCallbacks.MatchConsume("{{$Type.Singular}}", h.GetName(), input.ID) {
          forced = true
        }
      }

      triggered := m
      if h.Trigger != nil {
        triggered = h.Trigger.Intersect(m)
      }

      if triggered == ({{$Type.Singular}}FieldMask{}) && !forced {
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
          log.Add("{{$Type.Singular}}", h.GetName(), input.ID)
        }

        if err := h.Func(modelutil.WithPathEntry(ctx, fmt.Sprintf("CB#"+h.GetQualifiedName()+"#{{FormatTemplate $Type.IDField.GoType}}", input.ID)), tx, uid, euid, options, &c, input); err != nil {
          return nil, fmt.Errorf("{{$Type.Singular}}APICreate: BeforeSave callback %s for %s failed: %w", h.Name, input.ID, err)
        }
      }

      traceregistry.Add(ctx, traceregistry.EventCallbackComplete{
        ID: uuid.Must(uuid.NewV4()),
        Time: time.Now(),
        Name: h.GetQualifiedName(),
        Duration: time.Now().Sub(before),
        Changed: {{$Type.Singular}}FieldMaskFrom(&a, input).Changes(&a, input),
      })

      exitCallback()
    }

    exitIteration()
  }

{{range $Field := $Type.Fields}}
  {{- if (and $Field.Array (not $Field.IsNull))}}
    if input.{{$Field.GoName}} == nil {
      input.{{$Field.GoName}} = make({{$Field.GoType}}, 0)
    }
  {{- end}}

  {{- if not $Field.IgnoreCreate }}
    {{- if $Field.Enum}}
      {{- if $Field.Array}}
        for i, v := range input.{{$Field.GoName}} {
          if !{{(PackageName "enum" $Type.Singular)}}.Valid{{$Field.GoName}}[v] {
            return nil, fmt.Errorf("{{$Type.Singular}}APICreate: value for member %d of field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", i, {{(PackageName "enum" $Type.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
          }
        }
      {{- else}}
        if !{{(PackageName "enum" $Type.Singular)}}.Valid{{$Field.GoName}}[input.{{$Field.GoName}}] {
          return nil, fmt.Errorf("{{$Type.Singular}}APICreate: value for field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", {{(PackageName "enum" $Type.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
        }
      {{- end}}
    {{- end}}

    ic[{{(PackageName "schema" $Type.Singular)}}.Column{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(input.{{$Field.GoName}}){{else}}input.{{$Field.GoName}}{{end}})

    {{- if $Type.HasAudit}}
      fields["{{$Field.GoName}}"] = []interface{}{input.{{$Field.GoName}}}
    {{- end}}
  {{- else if not (or $Field.IgnoreCreate $Field.IsNull) }}
    {{- if $Field.Array}}
      empty{{$Field.GoName}} := make({{$Field.GoType}}, 0)
    {{- else}}
      var empty{{$Field.GoName}} {{$Field.GoType}}
    {{- end}}

    ic[{{(PackageName "schema" $Type.Singular)}}.Column{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(empty{{$Field.GoName}}){{else}}empty{{$Field.GoName}}{{end}})
  {{- end}}
{{- end}}

  qb := sqlbuilder.Insert().Table({{(PackageName "schema" $Type.Singular)}}.Table).Columns(ic)

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APICreate: couldn't generate query: %w", err)
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APICreate: couldn't perform query: %w", err)
  }

{{if $Type.HasVersion}}
  if _, err := tx.ExecContext(ctx, "select pg_notify('model_changes', $1)", fmt.Sprintf("{{$Type.Singular}}/%s/%d", input.ID, input.Version)); err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APICreate: couldn't send postgres notification: %w", err)
  }
{{end}}

  v, err := {{$Type.Singular}}APIGet(ctx, tx, input.ID, &uid, &euid)
  if err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APICreate: couldn't get object after creation: %w", err)
  }

  changeregistry.Add(ctx, "{{$Type.Singular}}", input.ID)

{{if $Type.HasAudit}}
  if err := modelutil.RecordAuditEvent(ctx, tx, uuid.Must(uuid.NewV4()), time.Now(), uid, euid, "create", "{{$Type.Singular}}", input.ID, fields); err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APICreate: couldn't create audit record: %w", err)
  }
{{end}}

  if queue != nil {
    if err := queue.Run(ctx, tx); err != nil {
      return nil, fmt.Errorf("{{$Type.Singular}}APICreate: couldn't run callback queue: %w", err)
    }

    vv, err := {{$Type.Singular}}APIGet(ctx, tx, input.ID, &uid, &euid)
    if err != nil {
      return nil, fmt.Errorf("{{$Type.Singular}}APICreate: couldn't get object after running callback queue: %w", err)
    }

    v = vv
  }

  return v, nil
}

func {{$Type.Singular}}APIHandleCreate(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid uuid.UUID) {
  var input {{$Type.Singular}}

  switch r.Header.Get("content-type") {
  default:
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  }

  ctx := modelutil.WithPathEntry(r.Context(), fmt.Sprintf("HTTP#{{$Type.Singular}}Create#{{FormatTemplate $Type.IDField.GoType}}", input.ID))

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

  v, err := {{$Type.Singular}}APICreate(ctx, mctx, tx, uid, euid, time.Now(), &input, options)
  if err != nil {
    panic(err)
  }

  var result struct {
    Time time.Time "json:\"time\""
    Record *{{$Type.Singular}} "json:\"record\""
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

func {{$Type.Singular}}APIHandleCreateMultiple(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid uuid.UUID) {
  var input struct { Records []{{$Type.Singular}} "json:\"records\"" }
  var output struct {
    Time time.Time "json:\"time\""
    Records []{{$Type.Singular}} "json:\"records\""
    Changed map[string][]interface{} "json:\"changed\""
  }

  switch r.Header.Get("content-type") {
  default:
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  }

  ctx := modelutil.WithPathEntry(r.Context(), "HTTP#{{$Type.Singular}}CreateMultiple")

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
    v, err := {{$Type.Singular}}APICreate(ctx, mctx, tx, uid, euid, time.Now(), &input.Records[i], options)
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

{{if $Type.HasAPIUpdate}}
func (jsctx *JSContext) {{$Type.Singular}}Save(input *{{$Type.Singular}}) *{{$Type.Singular}} {
  v, err := {{$Type.Singular}}APISave(modelutil.WithPathEntry(jsctx.ctx, fmt.Sprintf("JS#{{$Type.Singular}}Save#{{FormatTemplate $Type.IDField.GoType}}", input.ID)), jsctx.mctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), input, nil)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (jsctx *JSContext) {{$Type.Singular}}SaveWithOptions(input *{{$Type.Singular}}, options *modelutil.APIOptions) *{{$Type.Singular}} {
  v, err := {{$Type.Singular}}APISave(modelutil.WithPathEntry(jsctx.ctx, fmt.Sprintf("JS#{{$Type.Singular}}SaveWithOptions#{{FormatTemplate $Type.IDField.GoType}}", input.ID)), jsctx.mctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), input, options)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (v *{{$Type.Singular}}) APISave(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, uid, euid uuid.UUID, now time.Time, options *modelutil.APIOptions) error {
  vv, err := {{$Type.Singular}}APISave(ctx, mctx, tx, uid, euid, now, v, options)
  if err != nil {
    return fmt.Errorf("{{$Type.Singular}}.APISave: %w", err)
  } else if vv == nil {
    return fmt.Errorf("{{$Type.Singular}}.APISave: {{$Type.Singular}}APISave did not return a valid record")
  }

  *v = *vv

  return nil
}

func {{$Type.Singular}}APISave(
  ctx context.Context,
  mctx *modelutil.ModelContext,
  tx *sql.Tx,
  uid, euid uuid.UUID,
  now time.Time,
  input *{{$Type.Singular}},
  options *modelutil.APIOptions,
) (*{{$Type.Singular}}, error) {
  if input.ID == {{if (EqualStrings $Type.IDField.GoType "int")}}0{{else}}uuid.Nil{{end}} {
    return nil, fmt.Errorf("{{$Type.Singular}}APISave: ID field was empty")
  }

  ctx, queue := modelutil.WithDeferredCallbackQueue(ctx)
  ctx, log := modelutil.WithCallbackHistoryLog(ctx)
  ctx = modelutil.WithPathEntry(ctx, fmt.Sprintf("API#{{$Type.Singular}}Save#{{FormatTemplate $Type.IDField.GoType}}", input.ID))

  p, err := {{$Type.Singular}}APIGet(ctx, tx, input.ID, &uid, &euid)
  if err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APISave: couldn't fetch previous state: %w", err)
  }

{{if $Type.HasVersion}}
  if input.Version != p.Version {
    return nil, fmt.Errorf("{{$Type.Singular}}APISave: Version from input did not match current state (input=%d current=%d): %w", input.Version, p.Version, ErrVersionMismatch)
  }
{{end}}

{{range $Field := $Type.Fields}}
{{- if $Field.IgnoreUpdate }}
  input.{{$Field.GoName}} = p.{{$Field.GoName}}
{{- end}}
{{- if $Field.Enum}}
{{- if $Field.Array}}
  for i, v := range input.{{$Field.GoName}} {
    if !{{(PackageName "enum" $Type.Singular)}}.Valid{{$Field.GoName}}[v] {
      return nil, fmt.Errorf("{{$Type.Singular}}APISave: value for member %d of field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", i, {{(PackageName "enum" $Type.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
    }
  }
{{- else}}
  if !{{(PackageName "enum" $Type.Singular)}}.Valid{{$Field.GoName}}[input.{{$Field.GoName}}] {
    return nil, fmt.Errorf("{{$Type.Singular}}APISave: value for field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", {{(PackageName "enum" $Type.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
  }
{{- end}}
{{- end}}
{{- end}}

  exitActivity := traceregistry.Enter(ctx, &traceregistry.EventModelActivity{
    ID: uuid.Must(uuid.NewV4()),
    Time: time.Now(),
    Action: "save",
    ModelType: "{{$Type.Singular}}",
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
      h, ok := e.({{$Type.Singular}}BeforeSaveHandler)
      if !ok {
        continue
      }

      if log != nil && log.Has("{{$Type.Singular}}", h.GetName(), input.ID) {
        continue
      }

      if options.ForceCallbacks.Match("{{$Type.Singular}}", h.GetName(), input.ID) {
        forcing = true
      }
    }
  }

  for {
    m := {{$Type.Singular}}FieldMaskFrom(&b, input)
    if m == ({{$Type.Singular}}FieldMask{}) && !(n == 0 && forcing) {
      break
    }
    c := b
    b = *input

    n++
    if n > 100 {
      return nil, fmt.Errorf("{{$Type.Singular}}APISave: BeforeSave callback for %s exceeded execution limit of 100 iterations", input.ID)
    }

    exitIteration := traceregistry.Enter(ctx, &traceregistry.EventIteration{
      ID: uuid.Must(uuid.NewV4()),
      Time: time.Now(),
      ObjectType: "{{$Type.Singular}}",
      ObjectID: input.ID,
      Number: n,
    })
    defer func() { exitIteration() }()

    for _, e := range mctx.GetHandlers() {
      h, ok := e.({{$Type.Singular}}BeforeSaveHandler)
      if !ok {
        continue
      }

      skipped := false
      forced := false

      if options != nil {
        if options.SkipCallbacks.MatchConsume("{{$Type.Singular}}", h.GetName(), input.ID) {
          skipped = true
        }
        if options.ForceCallbacks.MatchConsume("{{$Type.Singular}}", h.GetName(), input.ID) {
          forced = true
        }
      }

      triggered := m
      if h.Trigger != nil {
        triggered = h.Trigger.Intersect(m)
      }

      if triggered == ({{$Type.Singular}}FieldMask{}) && !forced {
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
          log.Add("{{$Type.Singular}}", h.GetName(), input.ID)
        }

        if err := h.Func(modelutil.WithPathEntry(ctx, fmt.Sprintf("CB#"+h.GetQualifiedName()+"#{{FormatTemplate $Type.IDField.GoType}}", input.ID)), tx, uid, euid, options, &c, input); err != nil {
          return nil, fmt.Errorf("{{$Type.Singular}}APISave: BeforeSave callback %s for %s failed: %w", h.Name, input.ID, err)
        }
      }

      traceregistry.Add(ctx, traceregistry.EventCallbackComplete{
        ID: uuid.Must(uuid.NewV4()),
        Time: time.Now(),
        Name: h.GetQualifiedName(),
        Duration: time.Now().Sub(before),
        Changed: {{$Type.Singular}}FieldMaskFrom(&a, input).Changes(&a, input),
      })

      exitCallback()
    }

    exitIteration()
  }

  uc := sqlbuilder.UpdateColumns{}

{{if $Type.HasAudit}}
  changed := make(map[string][]interface{})
{{- end}}

  skip := true

{{range $Field := $Type.Fields}}
{{- if not $Field.IgnoreUpdate}}
  if {{ (NotEqual (Join "input." $Field.GoName) $Field.GoType (Join "p." $Field.GoName) $Field.GoType) }} {
    skip = false

{{- if $Field.Enum}}
{{- if $Field.Array}}
    for i, v := range input.{{$Field.GoName}} {
      if !{{(PackageName "enum" $Type.Singular)}}.Valid{{$Field.GoName}}[v] {
        return nil, fmt.Errorf("{{$Type.Singular}}APISave: value for member %d of field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", i, {{(PackageName "enum" $Type.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
      }
    }
{{- else}}
    if !{{(PackageName "enum" $Type.Singular)}}.Valid{{$Field.GoName}}[input.{{$Field.GoName}}] {
      return nil, fmt.Errorf("{{$Type.Singular}}APISave: value for field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", {{(PackageName "enum" $Type.Singular)}}.Values{{$Field.GoName}}, input.{{$Field.GoName}})
    }
{{- end}}
{{- end}}

    uc[{{(PackageName "schema" $Type.Singular)}}.Column{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(input.{{$Field.GoName}}){{else}}input.{{$Field.GoName}}{{end}})
{{- if $Type.HasAudit}}
    changed["{{$Field.GoName}}"] = []interface{}{p.{{$Field.GoName}}, input.{{$Field.GoName}}}
{{- end}}
  }
{{- end}}
{{- end}}

  if skip == false {
{{- if $Type.HasVersion}}
    input.Version = input.Version + 1
    uc[{{(PackageName "schema" $Type.Singular)}}.ColumnVersion] = sqlbuilder.Bind(input.Version)
{{- if $Type.HasAudit}}
    if input.Version != p.Version {
      changed["Version"] = []interface{}{p.Version, input.Version}
    }
{{- end}}
{{- end}}
{{- if $Type.HasUpdatedAt}}
    input.UpdatedAt = now
    uc[{{(PackageName "schema" $Type.Singular)}}.ColumnUpdatedAt] = sqlbuilder.Bind(input.UpdatedAt)
{{- if $Type.HasAudit}}
    if !input.UpdatedAt.Equal(p.UpdatedAt) {
      changed["UpdatedAt"] = []interface{}{p.UpdatedAt, input.UpdatedAt}
    }
{{- end}}
{{- end}}
{{- if $Type.HasUpdaterID}}
    input.UpdaterID = euid
    uc[{{(PackageName "schema" $Type.Singular)}}.ColumnUpdaterID] = sqlbuilder.Bind(input.UpdaterID)
{{- if $Type.HasAudit}}
    if input.UpdaterID != p.UpdaterID {
      changed["UpdaterID"] = []interface{}{p.UpdaterID, input.UpdaterID}
    }
{{- end}}
{{- end}}

    qb := sqlbuilder.Update().Table({{(PackageName "schema" $Type.Singular)}}.Table).Set(uc).Where(sqlbuilder.Eq({{(PackageName "schema" $Type.Singular)}}.ColumnID, sqlbuilder.Bind(input.ID)))

    qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
    if err != nil {
      return nil, fmt.Errorf("{{$Type.Singular}}APISave: couldn't generate query: %w", err)
    }

    if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
      return nil, fmt.Errorf("{{$Type.Singular}}APISave: couldn't update record: %w", err)
    }

{{if $Type.HasVersion}}
    if _, err := tx.ExecContext(ctx, "select pg_notify('model_changes', $1)", fmt.Sprintf("{{$Type.Singular}}/%s/%d", input.ID, input.Version)); err != nil {
      return nil, fmt.Errorf("{{$Type.Singular}}APISave: couldn't send postgres notification: %w", err)
    }
{{end}}

    changeregistry.Add(ctx, "{{$Type.Singular}}", input.ID)

{{if $Type.HasAudit}}
    if err := modelutil.RecordAuditEvent(ctx, tx, uuid.Must(uuid.NewV4()), time.Now(), uid, euid, "update", "{{$Type.Singular}}", input.ID, changed); err != nil {
      return nil, fmt.Errorf("{{$Type.Singular}}APISave: couldn't create audit record: %w", err)
    }
{{end}}
  }

  if queue != nil {
    if err := queue.Run(ctx, tx); err != nil {
      return nil, fmt.Errorf("{{$Type.Singular}}APISave: couldn't run callback queue: %w", err)
    }

    vv, err := {{$Type.Singular}}APIGet(ctx, tx, input.ID, &uid, &euid)
    if err != nil {
      return nil, fmt.Errorf("{{$Type.Singular}}APISave: couldn't get object after running callback queue: %w", err)
    }

    input = vv
  }

  return input, nil
}

func {{$Type.Singular}}APIHandleSave(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid uuid.UUID) {
  var input {{$Type.Singular}}

  switch r.Header.Get("content-type") {
  default:
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  }

  ctx := modelutil.WithPathEntry(r.Context(), fmt.Sprintf("HTTP#{{$Type.Singular}}Save#{{FormatTemplate $Type.IDField.GoType}}", input.ID))

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

  v, err := {{$Type.Singular}}APISave(ctx, mctx, tx, uid, euid, time.Now(), &input, options)
  if err != nil {
    panic(err)
  }

  var result struct {
    Time time.Time "json:\"time\""
    Record *{{$Type.Singular}} "json:\"record\""
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

func {{$Type.Singular}}APIHandleSaveMultiple(rw http.ResponseWriter, r *http.Request, mctx *modelutil.ModelContext, db *sql.DB, uid, euid uuid.UUID) {
  var input struct { Records []{{$Type.Singular}} "json:\"records\"" }
  var output struct {
    Time time.Time "json:\"time\""
    Records []{{$Type.Singular}} "json:\"records\""
    Changed map[string][]interface{} "json:\"changed\""
  }

  switch r.Header.Get("content-type") {
  default:
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  }

  ctx := modelutil.WithPathEntry(r.Context(), "HTTP#{{$Type.Singular}}SaveMultiple")

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
    v, err := {{$Type.Singular}}APISave(ctx, mctx, tx, uid, euid, time.Now(), &input.Records[i], options)
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

func {{$Type.Singular}}APIFindAndModify(
  ctx context.Context,
  mctx *modelutil.ModelContext,
  tx *sql.Tx,
  uid, euid uuid.UUID,
  now time.Time,
  id {{$Type.IDField.GoType}},
  options *modelutil.APIOptions,
  modify func(v *{{$Type.Singular}}) error,
) (*{{$Type.Singular}}, error) {
  v, err := {{$Type.Singular}}APIGet(ctx, tx, id, &uid, &euid)
  if err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APIFindAndModify: error fetching record {{$Type.IDField.FormatType}}: %w", id, err)
  } else if v == nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APIFindAndModify: could not find record {{$Type.IDField.FormatType}}", id)
  }

  if err := modify(v); err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APIFindAndModify: error modifying record {{$Type.IDField.FormatType}}: %w", id, err)
  }

  vv, err := {{$Type.Singular}}APISave(ctx, mctx, tx, uid, euid, now, v, options)
  if err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APIFindAndModify: error saving record {{$Type.IDField.FormatType}}: %w", id, err)
  }

  return vv, nil
}

func (v *{{$Type.Singular}}) APIFindAndModify(
  ctx context.Context,
  mctx *modelutil.ModelContext,
  tx *sql.Tx,
  uid, euid uuid.UUID,
  now time.Time,
  options *modelutil.APIOptions,
  modify func(v *{{$Type.Singular}}) error,
) error {
  vv, err := {{$Type.Singular}}APIFindAndModify(ctx, mctx, tx, uid, euid, now, v.ID, options, modify)
  if err != nil {
    return fmt.Errorf("{{$Type.Singular}}.APIFindAndModify: %w", err)
  }

  *v = *vv

  return nil
}

func {{$Type.Singular}}APIFindAndModifyOutsideTransaction(
  ctx context.Context,
  mctx *modelutil.ModelContext,
  db *sql.DB,
  uid, euid uuid.UUID,
  now time.Time,
  id {{$Type.IDField.GoType}},
  options *modelutil.APIOptions,
  modify func(v *{{$Type.Singular}}) error,
) (*{{$Type.Singular}}, error) {
  var out *{{$Type.Singular}}

  if err := retrydb.LinearBackoff(db, 5, time.Millisecond*500, func(tx *sql.Tx) error {
    v, err := {{$Type.Singular}}APIFindAndModify(ctx, mctx, tx, uid, euid, now, id, options, modify)
    if err != nil {
      return err
    }

    out = v

    return nil
  }); err != nil {
    return nil, fmt.Errorf("{{$Type.Singular}}APIFindAndModifyOutsideTransaction: %w", err)
  }

  return out, nil
}

func (v *{{$Type.Singular}}) APIFindAndModifyOutsideTransaction(
  ctx context.Context,
  mctx *modelutil.ModelContext,
  db *sql.DB,
  uid, euid uuid.UUID,
  now time.Time,
  options *modelutil.APIOptions,
  modify func(v *{{$Type.Singular}}) error,
) error {
  vv, err := {{$Type.Singular}}APIFindAndModifyOutsideTransaction(ctx, mctx, db, uid, euid, now, v.ID, options, modify)
  if err != nil {
    return fmt.Errorf("{{$Type.Singular}}.APIFindAndModifyOutsideTransaction: %w", err)
  }

  *v = *vv

  return nil
}
{{end}}

{{if $Type.HasCreatedAt}}
func (jsctx *JSContext) {{$Type.Singular}}ChangeCreatedAt(id {{$Type.IDField.GoType}}, createdAt time.Time) {
  if err := {{$Type.Singular}}APIChangeCreatedAt(jsctx.ctx, jsctx.mctx, jsctx.tx, id, createdAt); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}

func {{$Type.Singular}}APIChangeCreatedAt(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, id {{$Type.IDField.GoType}}, createdAt time.Time) error {
  if id == {{if (EqualStrings $Type.IDField.GoType "int")}}0{{else}}uuid.Nil{{end}} {
    return fmt.Errorf("{{$Type.Singular}}APIChangeCreatedAt: id was empty")
  }
  if createdAt.IsZero() {
    return fmt.Errorf("{{$Type.Singular}}APIChangeCreatedAt: createdAt was empty")
  }

  qb := sqlbuilder.Update().Table({{(PackageName "schema" $Type.Singular)}}.Table).Set(sqlbuilder.UpdateColumns{
    {{(PackageName "schema" $Type.Singular)}}.ColumnCreatedAt: sqlbuilder.Bind(createdAt),
  }).Where(sqlbuilder.Eq({{(PackageName "schema" $Type.Singular)}}.ColumnID, sqlbuilder.Bind(id)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return fmt.Errorf("{{$Type.Singular}}APIChangeCreatedAt: couldn't generate query: %w", err)
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return fmt.Errorf("{{$Type.Singular}}APIChangeCreatedAt: couldn't update record: %w", err)
  }

  return nil
}
{{end}}

{{if $Type.HasCreatorID}}
func (jsctx *JSContext) {{$Type.Singular}}ChangeCreatorID(id, creatorID uuid.UUID) {
  if err := {{$Type.Singular}}APIChangeCreatorID(jsctx.ctx, jsctx.mctx, jsctx.tx, id, creatorID); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}

func {{$Type.Singular}}APIChangeCreatorID(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, id, creatorID uuid.UUID) error {
  if id == {{if (EqualStrings $Type.IDField.GoType "int")}}0{{else}}uuid.Nil{{end}} {
    return fmt.Errorf("{{$Type.Singular}}APIChangeCreatorID: id was empty")
  }
  if creatorID == uuid.Nil {
    return fmt.Errorf("{{$Type.Singular}}APIChangeCreatorID: creatorID was empty")
  }

  qb := sqlbuilder.Update().Table({{(PackageName "schema" $Type.Singular)}}.Table).Set(sqlbuilder.UpdateColumns{
    {{(PackageName "schema" $Type.Singular)}}.ColumnCreatorID: sqlbuilder.Bind(creatorID),
  }).Where(sqlbuilder.Eq({{(PackageName "schema" $Type.Singular)}}.ColumnID, sqlbuilder.Bind(id)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return fmt.Errorf("{{$Type.Singular}}APIChangeCreatorID: couldn't generate query: %w", err)
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return fmt.Errorf("{{$Type.Singular}}APIChangeCreatorID: couldn't update record: %w", err)
  }

  return nil
}
{{end}}

{{if $Type.HasUpdatedAt}}
func (jsctx *JSContext) {{$Type.Singular}}ChangeUpdatedAt(id {{$Type.IDField.GoType}}, updatedAt time.Time) {
  if err := {{$Type.Singular}}APIChangeUpdatedAt(jsctx.ctx, jsctx.mctx, jsctx.tx, id, updatedAt); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}

func {{$Type.Singular}}APIChangeUpdatedAt(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, id {{$Type.IDField.GoType}}, updatedAt time.Time) error {
  if id == {{if (EqualStrings $Type.IDField.GoType "int")}}0{{else}}uuid.Nil{{end}} {
    return fmt.Errorf("{{$Type.Singular}}APIChangeUpdatedAt: id was empty")
  }
  if updatedAt.IsZero() {
    return fmt.Errorf("{{$Type.Singular}}APIChangeUpdatedAt: updatedAt was empty")
  }

  qb := sqlbuilder.Update().Table({{(PackageName "schema" $Type.Singular)}}.Table).Set(sqlbuilder.UpdateColumns{
    {{(PackageName "schema" $Type.Singular)}}.ColumnUpdatedAt: sqlbuilder.Bind(updatedAt),
  }).Where(sqlbuilder.Eq({{(PackageName "schema" $Type.Singular)}}.ColumnID, sqlbuilder.Bind(id)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return fmt.Errorf("{{$Type.Singular}}APIChangeUpdatedAt: couldn't generate query: %w", err)
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return fmt.Errorf("{{$Type.Singular}}APIChangeUpdatedAt: couldn't update record: %w", err)
  }

  return nil
}
{{end}}

{{if $Type.HasUpdaterID}}
func (jsctx *JSContext) {{$Type.Singular}}ChangeUpdaterID(id, updaterID uuid.UUID) {
  if err := {{$Type.Singular}}APIChangeUpdaterID(jsctx.ctx, jsctx.mctx, jsctx.tx, id, updaterID); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}

func {{$Type.Singular}}APIChangeUpdaterID(ctx context.Context, mctx *modelutil.ModelContext, tx *sql.Tx, id, updaterID uuid.UUID) error {
  if id == {{if (EqualStrings $Type.IDField.GoType "int")}}0{{else}}uuid.Nil{{end}} {
    return fmt.Errorf("{{$Type.Singular}}APIChangeUpdaterID: id was empty")
  }
  if updaterID == uuid.Nil {
    return fmt.Errorf("{{$Type.Singular}}APIChangeUpdaterID: updaterID was empty")
  }

  qb := sqlbuilder.Update().Table({{(PackageName "schema" $Type.Singular)}}.Table).Set(sqlbuilder.UpdateColumns{
    {{(PackageName "schema" $Type.Singular)}}.ColumnUpdaterID: sqlbuilder.Bind(updaterID),
  }).Where(sqlbuilder.Eq({{(PackageName "schema" $Type.Singular)}}.ColumnID, sqlbuilder.Bind(id)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return fmt.Errorf("{{$Type.Singular}}APIChangeUpdaterID: couldn't generate query: %w", err)
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return fmt.Errorf("{{$Type.Singular}}APIChangeUpdaterID: couldn't update record: %w", err)
  }

  return nil
}
{{end}}

{{range $Field := $Type.Fields}}
{{- if not (eq $Field.Sequence "")}}
func (jsctx *JSContext) {{$Type.Singular}}Set{{$Field.GoName}}IfEmpty(v *{{$Type.Singular}}) {
  if err := {{$Type.Singular}}APISet{{$Field.GoName}}IfEmpty(jsctx.ctx, jsctx.tx, v); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}

func {{$Type.Singular}}APISet{{$Field.GoName}}IfEmpty(ctx context.Context, tx *sql.Tx, v *{{$Type.Singular}}) error {
  if v.{{$Field.GoName}} != 0 {
    return nil
  }

  if err := tx.QueryRowContext(ctx, "select nextval('{{$Field.Sequence}}')").Scan(&v.{{$Field.GoName}}); err != nil {
    return fmt.Errorf("{{$Type.Singular}}APISet{{$Field.GoName}}IfEmpty: couldn't get sequence value for field \"{{$Field.APIName}}\" from sequence \"{{$Field.Sequence}}\": %w", err)
  }

  return nil
}
{{- end}}
{{end}}

{{range $Process := $Type.Processes}}
type {{$Type.Singular}}Process{{$Process}} struct { Value *{{$Type.Singular}} }

func (v *{{$Type.Singular}}) ProcessFor{{$Process}}() *{{$Type.Singular}}Process{{$Process}} {
  return &{{$Type.Singular}}Process{{$Process}}{Value: v}
}

func (p *{{$Type.Singular}}Process{{$Process}}) Name() string {
  return "{{$Type.Singular}}.{{$Process}}"
}
func (p *{{$Type.Singular}}Process{{$Process}}) GetStatus() string {
  return p.Value.{{$Process}}Status
}
func (p *{{$Type.Singular}}Process{{$Process}}) GetCompletedAt() *time.Time {
  return p.Value.{{$Process}}CompletedAt
}
func (p *{{$Type.Singular}}Process{{$Process}}) SetCompletedAt(completedAt *time.Time) {
  p.Value.{{$Process}}CompletedAt = completedAt
}
func (p *{{$Type.Singular}}Process{{$Process}}) GetStartedAt() *time.Time {
  return p.Value.{{$Process}}StartedAt
}
func (p *{{$Type.Singular}}Process{{$Process}}) SetStartedAt(startedAt *time.Time) {
  p.Value.{{$Process}}StartedAt = startedAt
}
func (p *{{$Type.Singular}}Process{{$Process}}) GetDeadline() *time.Time {
  return p.Value.{{$Process}}Deadline
}
func (p *{{$Type.Singular}}Process{{$Process}}) SetDeadline(deadline *time.Time) {
  p.Value.{{$Process}}Deadline = deadline
}
func (p *{{$Type.Singular}}Process{{$Process}}) GetFailureMessage() string {
  return p.Value.{{$Process}}FailureMessage
}
func (p *{{$Type.Singular}}Process{{$Process}}) SetFailureMessage(failureMessage string) {
  p.Value.{{$Process}}FailureMessage = failureMessage
}
{{end}}
`))
