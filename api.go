package main

import (
	"go/types"
	"io"
	"strings"
	"text/template"
)

type APIWriter struct{ Dir string }

func NewAPIWriter(dir string) *APIWriter { return &APIWriter{Dir: dir} }

func (APIWriter) Name() string     { return "api" }
func (APIWriter) Language() string { return "go" }
func (w APIWriter) File(typeName string, _ *types.Named, _ *types.Struct) string {
	return w.Dir + "/" + strings.ToLower(typeName) + "_api.go"
}

func (APIWriter) Imports() []string {
	return []string{
		"encoding/csv",
		"encoding/json",
		"fmt",
		"fknsrs.biz/p/civil",
		"fknsrs.biz/p/sqlbuilder",
		"github.com/gorilla/mux",
		"github.com/gorilla/schema",
		"github.com/pkg/errors",
		"github.com/satori/go.uuid",
		"github.com/shopspring/decimal",
		"github.com/timewasted/go-accept-headers",
		"github.com/vmihailenco/msgpack/v5",
		"movingdata.com/p/wbi/internal/apihelpers",
		"movingdata.com/p/wbi/internal/apifilter",
		"movingdata.com/p/wbi/internal/apitypes",
		"movingdata.com/p/wbi/internal/changeregistry",
		"movingdata.com/p/wbi/internal/cookiesession",
		"movingdata.com/p/wbi/internal/traceregistry",
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

{{- range $Field := $Type.Fields}}
{{- if $Field.Enum}}
const (
{{- range $Enum := $Field.Enum}}
  {{$Type.Singular}}Enum{{$Field.GoName}}{{$Enum.GoName}} = "{{$Enum.Value}}"
{{- end}}
)

var (
  {{$Type.Singular}}Valid{{$Field.GoName}} = map[string]bool{}
  {{$Type.Singular}}Values{{$Field.GoName}} = []string{}
  {{$Type.Singular}}Labels{{$Field.GoName}} = map[string]string{
{{- range $Enum := $Field.Enum}}
    {{$Type.Singular}}Enum{{$Field.GoName}}{{$Enum.GoName}}: "{{$Enum.Label}}",
{{- end}}
  }
)

func init() {
  for k := range {{$Type.Singular}}Labels{{$Field.GoName}} {
    {{$Type.Singular}}Valid{{$Field.GoName}}[k] = true
    {{$Type.Singular}}Values{{$Field.GoName}} = append({{$Type.Singular}}Values{{$Field.GoName}}, k)
  }
}
{{- end}}
{{- end}}

func init() {
  registerFinder("{{$Type.Singular}}", func(mctx *ModelContext, ctx context.Context, db RowQueryerContext, id uuid.UUID, uid, euid *uuid.UUID) (interface{}, error) {
    v, err := mctx.{{$Type.Singular}}APIGet(ctx, db, id, uid, euid)
    return v, err
  })
}

func (jsctx *JSContext) {{$Type.Singular}}Get(id uuid.UUID) *{{$Type.Singular}} {
  v, err := jsctx.mctx.{{$Type.Singular}}APIGet(jsctx.ctx, jsctx.tx, id, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (mctx *ModelContext) {{$Type.Singular}}APIGet(ctx context.Context, db RowQueryerContext, id uuid.UUID, uid, euid *uuid.UUID) (*{{$Type.Singular}}, error) {
  qb := sqlbuilder.Select().From({{$Type.Singular}}Table).Columns(columnsAsExpressions({{$Type.Singular}}Columns)...)

{{- if $Type.HasUserFilter}}
  qb = {{$Type.Singular}}UserFilter(qb, euid)
{{- end}}

  qb = qb.AndWhere(sqlbuilder.Eq({{$Type.Singular}}TableID, sqlbuilder.Bind(id)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APIGet: couldn't generate query")
  }

  var v {{$Type.Singular}}
  if err := db.QueryRowContext(ctx, qs, qv...).Scan({{range $i, $Field := $Type.Fields}}{{if $Field.Array}}pq.Array(&v.{{$Field.GoName}}){{else}}&v.{{$Field.GoName}}{{end}}, {{end}}); err != nil {
    if err == sql.ErrNoRows {
      return nil, nil
    }

    return nil, errors.Wrap(err, "{{$Type.Singular}}APIGet: couldn't perform query")
  }

{{range $i, $Field := $Type.Fields}}
{{if $Field.Masked}}
  v.{{$Field.GoName}} = strings.Repeat("*", len(v.{{$Field.GoName}}))
{{end}}
{{end}}

  return &v, nil
}

func (mctx *ModelContext) {{$Type.Singular}}APIHandleGet(rw http.ResponseWriter, r *http.Request, db *sql.DB, uid, euid *uuid.UUID) {
  vars := mux.Vars(r)

  id, err := uuid.FromString(vars["id"])
  if err != nil {
    panic(err)
  }

  v, err := mctx.{{$Type.Singular}}APIGet(r.Context(), db, id, uid, euid)
  if err != nil {
    panic(err)
  }

  if v == nil {
    http.Error(rw, fmt.Sprintf("{{$Type.Singular}} with id %q not found", id.String()), http.StatusNotFound)
    return
  }

  a := accept.Parse(r.Header.Get("accept"))

  f, _ := a.Negotiate("application/json", "application/msgpack")

  switch f {
  case "application/msgpack":
    rw.Header().Set("content-type", "application/msgpack")
    rw.WriteHeader(http.StatusOK)

    if err := msgpack.NewEncoder(rw).Encode(v); err != nil {
      panic(err)
    }
  default:
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
}

type {{$Type.Singular}}APIFilterParameters struct {
{{- range $Field := $Type.Fields}}
{{- range $Filter := $Field.Filters}}
  {{$Filter.GoName}} {{$Filter.GoType}} "schema:\"{{$Filter.Name}}\" json:\"{{$Filter.Name}},omitempty\" api_filter:\"{{$Field.SQLName}},{{$Filter.Operator}}\""
{{- end}}
{{- end}}
{{- range $Filter := $Type.SpecialFilters}}
  {{$Filter.GoName}} {{$Filter.GoType}} "schema:\"{{$Filter.Name}}\" json:\"{{$Filter.Name}},omitempty\""
{{- end}}
}

func (p *{{$Type.Singular}}APIFilterParameters) Decode(q url.Values) error {
  return decodeStruct(q, p)
}

func (p *{{$Type.Singular}}APIFilterParameters) AddFilters(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement {
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

type {{$Type.Singular}}APISearchParameters struct {
  {{$Type.Singular}}APIFilterParameters
  Order *string "schema:\"order\" json:\"order,omitempty\""
  Offset *int "schema:\"offset\" json:\"offset,omitempty\""
  Limit *int "schema:\"limit\" json:\"limit,omitempty\""
}

func (p *{{$Type.Singular}}APISearchParameters) Decode(q url.Values) error {
  return decodeStruct(q, p)
}

func (p *{{$Type.Singular}}APISearchParameters) AddFilters(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement {
  if p == nil {
    return q
  }

  return p.{{$Type.Singular}}APIFilterParameters.AddFilters(q)
}

func (p *{{$Type.Singular}}APISearchParameters) AddLimits(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement {
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

type {{$Type.Singular}}APISearchResponse struct {
  Records []*{{$Type.Singular}} "json:\"records\""
  Total int "json:\"total\""
  Time time.Time "json:\"time\""
}

func (jsctx *JSContext) {{$Type.Singular}}Search(p {{$Type.Singular}}APISearchParameters) *{{$Type.Singular}}APISearchResponse {
  v, err := jsctx.mctx.{{$Type.Singular}}APISearch(jsctx.ctx, jsctx.tx, &p, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (mctx *ModelContext) {{$Type.Singular}}APISearch(ctx context.Context, db QueryerContextAndRowQueryerContext, p *{{$Type.Singular}}APISearchParameters, uid, euid *uuid.UUID) (*{{$Type.Singular}}APISearchResponse, error) {
  qb := sqlbuilder.Select().From({{$Type.Singular}}Table).Columns(columnsAsExpressions({{$Type.Singular}}Columns)...)

{{- if $Type.HasUserFilter}}
  qb = {{$Type.Singular}}UserFilter(qb, euid)
{{- end}}

  qb = p.AddFilters(qb)

  qb1 := p.AddLimits(qb)
  qs1, qv1, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb1.AsStatement).ToSQL()
  if err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APISearch: couldn't generate result query")
  }

  qb2 := qb.Columns(sqlbuilder.Func("count", sqlbuilder.Literal("*")))
  qs2, qv2, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb2.AsStatement).ToSQL()
  if err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APISearch: couldn't generate summary query")
  }

  rows, err := db.QueryContext(ctx, qs1, qv1...)
  if err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APISearch: couldn't perform result query")
  }
  defer rows.Close()

  a := make([]*{{$Type.Singular}}, 0)
  for rows.Next() {
    var m {{$Type.Singular}}
    if err := rows.Scan({{range $i, $Field := $Type.Fields}}{{if $Field.Array}}pq.Array(&m.{{$Field.GoName}}){{else}}&m.{{$Field.GoName}}{{end}} /* {{$i}} */, {{end}}); err != nil {
      return nil, errors.Wrap(err, "{{$Type.Singular}}APISearch: couldn't scan result row")
    }

{{range $i, $Field := $Type.Fields}}
{{if $Field.Masked}}
    m.{{$Field.GoName}} = strings.Repeat("*", len(m.{{$Field.GoName}}))
{{end}}
{{end}}

    a = append(a, &m)
  }

  if err := rows.Close(); err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APISearch: couldn't close result row set")
  }

  var total int
  if err := db.QueryRowContext(ctx, qs2, qv2...).Scan(&total); err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APISearch: couldn't perform summary query")
  }

  return &{{$Type.Singular}}APISearchResponse{
    Records: a,
    Total: total,
    Time: time.Now(),
  }, nil
}

func (jsctx *JSContext) {{$Type.Singular}}Find(p {{$Type.Singular}}APIFilterParameters) *{{$Type.Singular}} {
  v, err := jsctx.mctx.{{$Type.Singular}}APIFind(jsctx.ctx, jsctx.tx, &p, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (mctx *ModelContext) {{$Type.Singular}}APIFind(ctx context.Context, db QueryerContextAndRowQueryerContext, p *{{$Type.Singular}}APIFilterParameters, uid, euid *uuid.UUID) (*{{$Type.Singular}}, error) {
  qb := sqlbuilder.Select().From({{$Type.Singular}}Table).Columns(columnsAsExpressions({{$Type.Singular}}Columns)...)

{{- if $Type.HasUserFilter}}
  qb = {{$Type.Singular}}UserFilter(qb, euid)
{{- end}}

  qb = p.AddFilters(qb)

  qs1, qv1, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APIFind: couldn't generate result query")
  }

  qb2 := qb.Columns(sqlbuilder.Func("count", sqlbuilder.Literal("*")))
  qs2, qv2, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb2.AsStatement).ToSQL()
  if err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APIFind: couldn't generate summary query")
  }

  var m {{$Type.Singular}}
  if err := db.QueryRowContext(ctx, qs1, qv1...).Scan({{range $i, $Field := $Type.Fields}}{{if $Field.Array}}pq.Array(&m.{{$Field.GoName}}){{else}}&m.{{$Field.GoName}}{{end}}, {{end}}); err != nil {
    if err == sql.ErrNoRows {
      return nil, nil
    }

    return nil, errors.Wrap(err, "{{$Type.Singular}}APIFind: couldn't scan result row")
  }

  var total int
  if err := db.QueryRowContext(ctx, qs2, qv2...).Scan(&total); err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APIFind: couldn't perform summary query")
  }

  if total != 1 {
    return nil, errors.Errorf("{{$Type.Singular}}APIFind: expected one result, got %d", total)
  }

  return &m, nil
}

func (mctx *ModelContext) {{$Type.Singular}}APIHandleSearch(rw http.ResponseWriter, r *http.Request, db *sql.DB, uid, euid *uuid.UUID) {
  var p {{$Type.Singular}}APISearchParameters
  if err := decodeStruct(r.URL.Query(), &p); err != nil {
    panic(err)
  }

  v, err := mctx.{{$Type.Singular}}APISearch(r.Context(), db, &p, uid, euid)
  if err != nil {
    panic(err)
  }

  a := accept.Parse(r.Header.Get("accept"))

  f, _ := a.Negotiate("application/json", "application/msgpack")

  switch f {
  case "application/msgpack":
    rw.Header().Set("content-type", "application/msgpack")
    rw.WriteHeader(http.StatusOK)

    if err := msgpack.NewEncoder(rw).Encode(v); err != nil {
      panic(err)
    }
  default:
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
}

func (mctx *ModelContext) {{$Type.Singular}}APIHandleSearchCSV(rw http.ResponseWriter, r *http.Request, db *sql.DB, uid, euid *uuid.UUID) {
  var p {{$Type.Singular}}APISearchParameters
  if err := decodeStruct(r.URL.Query(), &p); err != nil {
    panic(err)
  }

  v, err := mctx.{{$Type.Singular}}APISearch(r.Context(), db, &p, uid, euid)
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

{{if (or $Type.CanCreate $Type.CanUpdate)}}
type {{$Type.Singular}}FieldMask struct {
{{range $Field := $Type.Fields}}
  {{$Field.GoName}} bool
{{- end}}
}

func (m {{$Type.Singular}}FieldMask) Fields() []string {
  return fieldMaskTrueFields("{{$Type.Singular}}", m)
}

func (m {{$Type.Singular}}FieldMask) Union(other {{$Type.Singular}}FieldMask) {{$Type.Singular}}FieldMask {
  var out {{$Type.Singular}}FieldMask
  fieldMaskUnion(m, other, &out)
  return out
}

func (m {{$Type.Singular}}FieldMask) Intersect(other {{$Type.Singular}}FieldMask) {{$Type.Singular}}FieldMask {
  var out {{$Type.Singular}}FieldMask
  fieldMaskIntersect(m, other, &out)
  return out
}

func (m {{$Type.Singular}}FieldMask) Match(a, b *{{$Type.Singular}}) bool {
  return fieldMaskMatch(m, a, b)
}

func (m *{{$Type.Singular}}FieldMask) From(a, b *{{$Type.Singular}}) {
  fieldMaskFrom(a, b, m)
}

func (m {{$Type.Singular}}FieldMask) Changes(a, b *{{$Type.Singular}}) ([]traceregistry.Change) {
  return fieldMaskChanges(m, a, b)
}

func {{$Type.Singular}}FieldMaskFrom(a, b *{{$Type.Singular}}) {{$Type.Singular}}FieldMask {
  var m {{$Type.Singular}}FieldMask
  m.From(a, b)
  return m
}

type {{$Type.Singular}}BeforeSaveHandlerFunc func(ctx context.Context, tx *sql.Tx, uid, euid uuid.UUID, options *APIOptions, current, proposed *{{$Type.Singular}}) error

type {{$Type.Singular}}BeforeSaveHandler struct {
  Trigger *{{$Type.Singular}}FieldMask
  Name string
  Func {{$Type.Singular}}BeforeSaveHandlerFunc
  Output []FieldMask
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetName() string {
  return h.Name
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetQualifiedName() string {
  return "{{$Type.Singular}}." + h.GetName()
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetInputs() []string {
  if h.Trigger != nil {
    return h.Trigger.Fields()
  }

  return []string{ {{range $Field := $Type.Fields}}"{{$Type.Singular}}.{{$Field.GoName}}",{{end}} }
}

func (h {{$Type.Singular}}BeforeSaveHandler) GetOutputs() []string {
  var a []string

  for _, e := range h.Output {
    a = append(a, e.Fields()...)
  }

  return a
}

func (h *{{$Type.Singular}}BeforeSaveHandler) Match(a, b *{{$Type.Singular}}) bool {
  if h.Trigger == nil {
    return true
  }

  return h.Trigger.Match(a, b)
}

func (mctx *ModelContext) {{$Type.Singular}}BeforeSave(trigger *{{$Type.Singular}}FieldMask, name string, fn {{$Type.Singular}}BeforeSaveHandlerFunc, output ...FieldMask) {
  mctx.handlers = append(mctx.handlers, {{$Type.Singular}}BeforeSaveHandler{
    Trigger: trigger,
    Name: name,
    Func: fn,
    Output: output,
  })
}
{{end}}

{{if $Type.CanCreate}}
func (jsctx *JSContext) {{$Type.Singular}}Create(input {{$Type.Singular}}) *{{$Type.Singular}} {
  v, err := jsctx.mctx.{{$Type.Singular}}APICreate(jsctx.ctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), &input, nil)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (jsctx *JSContext) {{$Type.Singular}}CreateWithOptions(input {{$Type.Singular}}, options APIOptions) *{{$Type.Singular}} {
  v, err := jsctx.mctx.{{$Type.Singular}}APICreate(jsctx.ctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), &input, &options)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (mctx *ModelContext) {{$Type.Singular}}APICreate(ctx context.Context, tx *sql.Tx, uid, euid uuid.UUID, now time.Time, input *{{$Type.Singular}}, options *APIOptions) (*{{$Type.Singular}}, error) {
  if !truthy(input.ID) {
    return nil, errors.Errorf("{{$Type.Singular}}APICreate: ID field was empty")
  }

  ic := sqlbuilder.InsertColumns{}

{{if $Type.HasAudit}}
  fields := make(map[string][]interface{})
{{- end}}

{{range $Field := $Type.Fields}}
{{- if $Field.Enum}}
{{- if $Field.Array}}
  for i, v := range input.{{$Field.GoName}} {
    if !{{$Type.Singular}}Valid{{$Field.GoName}}[v] {
      return nil, errors.Errorf("{{$Type.Singular}}APICreate: value for member %d of field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", i, {{$Type.Singular}}Values{{$Field.GoName}}, input.{{$Field.GoName}})
    }
  }
{{- else}}
  if !{{$Type.Singular}}Valid{{$Field.GoName}}[input.{{$Field.GoName}}] {
    return nil, errors.Errorf("{{$Type.Singular}}APICreate: value for field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", {{$Type.Singular}}Values{{$Field.GoName}}, input.{{$Field.GoName}})
  }
{{- end}}
{{- end}}
{{end}}

{{range $Field := $Type.Fields}}
{{- if not (eq $Field.Sequence "")}}
  if err := tx.QueryRowContext(ctx, "select nextval('{{$Field.Sequence}}')").Scan(&input.{{$Field.GoName}}); err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APICreate: couldn't get sequence value for field \"{{$Field.APIName}}\" from sequence \"{{$Field.Sequence}}\"")
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

{{if $Type.HasVersion -}}
  input.Version = 1
  ic[{{$Type.Singular}}TableVersion] = sqlbuilder.Bind(input.Version)
{{- if $Type.HasAudit}}
  fields["Version"] = []interface{}{input.Version}
{{- end}}
{{- end}}
{{if $Type.HasCreatedAt -}}
  input.CreatedAt = now
  ic[{{$Type.Singular}}TableCreatedAt] = sqlbuilder.Bind(input.CreatedAt)
{{- if $Type.HasAudit}}
  fields["CreatedAt"] = []interface{}{input.CreatedAt}
{{- end}}
{{- end}}
{{if $Type.HasUpdatedAt -}}
  input.UpdatedAt = now
  ic[{{$Type.Singular}}TableUpdatedAt] = sqlbuilder.Bind(input.UpdatedAt)
{{- if $Type.HasAudit}}
  fields["UpdatedAt"] = []interface{}{input.UpdatedAt}
{{- end}}
{{- end}}
{{if $Type.HasCreatorID -}}
  input.CreatorID = euid
  ic[{{$Type.Singular}}TableCreatorID] = sqlbuilder.Bind(input.CreatorID)
{{- if $Type.HasAudit}}
  fields["CreatorID"] = []interface{}{input.CreatorID}
{{- end}}
{{- end}}
{{if $Type.HasUpdaterID -}}
  input.UpdaterID = euid
  ic[{{$Type.Singular}}TableUpdaterID] = sqlbuilder.Bind(input.UpdaterID)
{{- if $Type.HasAudit}}
  fields["UpdaterID"] = []interface{}{input.UpdaterID}
{{- end}}
{{- end}}

  exitActivity := traceregistry.Enter(ctx, &traceregistry.EventModelActivity{
    ID: uuid.NewV4(),
    Time: time.Now(),
    Action: "create",
    ModelType: "{{$Type.Singular}}",
    ModelID: input.ID,
    ModelData: input,
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
      return nil, errors.Errorf("{{$Type.Singular}}APICreate: BeforeSave callback for %s exceeded execution limit of 100 iterations", input.ID)
    }

    exitIteration := traceregistry.Enter(ctx, &traceregistry.EventIteration{
      ID: uuid.NewV4(),
      Time: time.Now(),
      ObjectType: "{{$Type.Singular}}",
      ObjectID: input.ID,
      Number: n,
    })
    defer func() { exitIteration() }()

    for _, e := range mctx.handlers {
      h, ok := e.({{$Type.Singular}}BeforeSaveHandler)
      if !ok {
        continue
      }

      skipped := false
      forced := false

      if options != nil {
        if options.SkipCallbacks.Match("{{$Type.Singular}}", h.GetName(), input.ID) {
          skipped = true
        }
        if options.ForceCallbacks.Match("{{$Type.Singular}}", h.GetName(), input.ID) {
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
        ID: uuid.NewV4(),
        Time: before,
        Name: h.GetQualifiedName(),
        Skipped: skipped,
        Forced: forced,
        Triggered: triggerChanges,
      })
      defer func() { exitCallback() }()

      a := *input

      if !skipped || forced {
        if err := h.Func(ctx, tx, uid, euid, options, &c, input); err != nil {
          return nil, errors.Wrapf(err, "{{$Type.Singular}}APICreate: BeforeSave callback %s for %s failed", h.Name, input.ID)
        }
      }

      traceregistry.Add(ctx, traceregistry.EventCallbackComplete{
        ID: uuid.NewV4(),
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

  {{- if not (or $Field.IgnoreInput) }}
    {{- if $Field.Enum}}
      {{- if $Field.Array}}
        for i, v := range input.{{$Field.GoName}} {
          if !{{$Type.Singular}}Valid{{$Field.GoName}}[v] {
            return nil, errors.Errorf("{{$Type.Singular}}APICreate: value for member %d of field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", i, {{$Type.Singular}}Values{{$Field.GoName}}, input.{{$Field.GoName}})
          }
        }
      {{- else}}
        if !{{$Type.Singular}}Valid{{$Field.GoName}}[input.{{$Field.GoName}}] {
          return nil, errors.Errorf("{{$Type.Singular}}APICreate: value for field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", {{$Type.Singular}}Values{{$Field.GoName}}, input.{{$Field.GoName}})
        }
      {{- end}}
    {{- end}}

    ic[{{$Type.Singular}}Table{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(input.{{$Field.GoName}}){{else}}input.{{$Field.GoName}}{{end}})

    {{- if $Type.HasAudit}}
      fields["{{$Field.GoName}}"] = []interface{}{input.{{$Field.GoName}}}
    {{- end}}
  {{- else if not (or $Field.IgnoreInput $Field.IsNull) }}
    {{- if $Field.Array}}
      empty{{$Field.GoName}} := make({{$Field.GoType}}, 0)
    {{- else}}
      var empty{{$Field.GoName}} {{$Field.GoType}}
    {{- end}}

    ic[{{$Type.Singular}}Table{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(empty{{$Field.GoName}}){{else}}empty{{$Field.GoName}}{{end}})
  {{- end}}
{{- end}}

  qb := sqlbuilder.Insert().Table({{$Type.Singular}}Table).Columns(ic)

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APICreate: couldn't generate query")
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APICreate: couldn't perform query")
  }

{{if $Type.HasVersion}}
  if _, err := tx.ExecContext(ctx, "select pg_notify('model_changes', $1)", fmt.Sprintf("{{$Type.Singular}}/%s/%d", input.ID, input.Version)); err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APICreate: couldn't send postgres notification")
  }
{{end}}

  v, err := mctx.{{$Type.Singular}}APIGet(ctx, tx, input.ID, &uid, &euid)
  if err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APICreate: couldn't get object after creation")
  }

  changeregistry.Add(ctx, "{{$Type.Singular}}", input.ID)

{{if $Type.HasAudit}}
  if err := RecordAuditEvent(ctx, tx, uuid.NewV4(), time.Now(), uid, euid, "create", "{{$Type.Singular}}", input.ID, fields); err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APICreate: couldn't create audit record")
  }
{{end}}

  return v, nil
}

func (mctx *ModelContext) {{$Type.Singular}}APIHandleCreate(rw http.ResponseWriter, r *http.Request, db *sql.DB, uid, euid uuid.UUID) {
  var input {{$Type.Singular}}

  switch r.Header.Get("content-type") {
  case "application/msgpack":
    if err := msgpack.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  default:
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  }

  tx, err := db.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable})
  if err != nil {
    panic(err)
  }
  defer tx.Rollback()

  if _, err := tx.ExecContext(r.Context(), "set constraints all deferred"); err != nil {
    panic(err)
  }

  options, err := APIOptionsFromRequest(r)
  if err != nil {
    panic(err)
  }

  v, err := mctx.{{$Type.Singular}}APICreate(r.Context(), tx, uid, euid, time.Now(), &input, options)
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
      v, err := Find(k, mctx, r.Context(), tx, id, &uid, &euid)
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

  a := accept.Parse(r.Header.Get("accept"))

  f, _ := a.Negotiate("application/json", "application/msgpack")

  switch f {
  case "application/msgpack":
    rw.Header().Set("content-type", "application/msgpack")
    rw.WriteHeader(http.StatusOK)

    if err := msgpack.NewEncoder(rw).Encode(result); err != nil {
      panic(err)
    }
  default:
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
}

func (mctx *ModelContext) {{$Type.Singular}}APIHandleCreateMultiple(rw http.ResponseWriter, r *http.Request, db *sql.DB, uid, euid uuid.UUID) {
  var input struct { Records []{{$Type.Singular}} "json:\"records\"" }
  var output struct {
    Time time.Time "json:\"time\""
    Records []{{$Type.Singular}} "json:\"records\""
    Changed map[string][]interface{} "json:\"changed\""
  }

  switch r.Header.Get("content-type") {
  case "application/msgpack":
    if err := msgpack.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  default:
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  }

  tx, err := db.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable})
  if err != nil {
    panic(err)
  }
  defer tx.Rollback()

  if _, err := tx.ExecContext(r.Context(), "set constraints all deferred"); err != nil {
    panic(err)
  }

  options, err := APIOptionsFromRequest(r)
  if err != nil {
    panic(err)
  }

  for i := range input.Records {
    v, err := mctx.{{$Type.Singular}}APICreate(r.Context(), tx, uid, euid, time.Now(), &input.Records[i], options)
    if err != nil {
      panic(err)
    }

    output.Records = append(output.Records, *v)
  }

  output.Time = time.Now()
  output.Changed = make(map[string][]interface{})

  for k, l := range changeregistry.ChangesFromRequest(r) {
    for _, id := range l {
      v, err := Find(k, mctx, r.Context(), tx, id, &uid, &euid)
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

  a := accept.Parse(r.Header.Get("accept"))

  f, _ := a.Negotiate("application/json", "application/msgpack")

  switch f {
  case "application/msgpack":
    rw.Header().Set("content-type", "application/msgpack")
    rw.WriteHeader(http.StatusOK)

    if err := msgpack.NewEncoder(rw).Encode(output); err != nil {
      panic(err)
    }
  default:
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
}
{{end}}

{{if $Type.CanUpdate}}
func (jsctx *JSContext) {{$Type.Singular}}Save(input *{{$Type.Singular}}) *{{$Type.Singular}} {
  v, err := jsctx.mctx.{{$Type.Singular}}APISave(jsctx.ctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), input, nil)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (jsctx *JSContext) {{$Type.Singular}}SaveWithOptions(input *{{$Type.Singular}}, options *APIOptions) *{{$Type.Singular}} {
  v, err := jsctx.mctx.{{$Type.Singular}}APISave(jsctx.ctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), input, options)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (mctx *ModelContext) {{$Type.Singular}}APISave(ctx context.Context, tx *sql.Tx, uid, euid uuid.UUID, now time.Time, input *{{$Type.Singular}}, options *APIOptions) (*{{$Type.Singular}}, error) {
  if !truthy(input.ID) {
    return nil, errors.Errorf("{{$Type.Singular}}APISave: ID field was empty")
  }

  p, err := mctx.{{$Type.Singular}}APIGet(ctx, tx, input.ID, &uid, &euid)
  if err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APISave: couldn't fetch previous state")
  }

{{if $Type.HasVersion}}
  if input.Version != p.Version {
    return nil, errors.Wrapf(ErrVersionMismatch, "{{$Type.Singular}}APISave: Version from input did not match current state (input=%d current=%d)", input.Version, p.Version)
  }
{{end}}

{{range $Field := $Type.Fields}}
{{- if $Field.IgnoreInput }}
  input.{{$Field.GoName}} = p.{{$Field.GoName}}
{{- end}}
{{- if $Field.Enum}}
{{- if $Field.Array}}
  for i, v := range input.{{$Field.GoName}} {
    if !{{$Type.Singular}}Valid{{$Field.GoName}}[v] {
      return nil, errors.Errorf("{{$Type.Singular}}APISave: value for member %d of field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", i, {{$Type.Singular}}Values{{$Field.GoName}}, input.{{$Field.GoName}})
    }
  }
{{- else}}
  if !{{$Type.Singular}}Valid{{$Field.GoName}}[input.{{$Field.GoName}}] {
    return nil, errors.Errorf("{{$Type.Singular}}APISave: value for field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", {{$Type.Singular}}Values{{$Field.GoName}}, input.{{$Field.GoName}})
  }
{{- end}}
{{- end}}
{{- end}}

  exitActivity := traceregistry.Enter(ctx, &traceregistry.EventModelActivity{
    ID: uuid.NewV4(),
    Time: time.Now(),
    Action: "save",
    ModelType: "{{$Type.Singular}}",
    ModelID: input.ID,
    ModelData: input,
  })
  defer func() { exitActivity() }()

  b := *p

  n := 0

  forcing := false
  if options != nil {
    for _, e := range mctx.handlers {
      h, ok := e.({{$Type.Singular}}BeforeSaveHandler)
      if !ok {
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
      return nil, errors.Errorf("{{$Type.Singular}}APISave: BeforeSave callback for %s exceeded execution limit of 100 iterations", input.ID)
    }

    exitIteration := traceregistry.Enter(ctx, &traceregistry.EventIteration{
      ID: uuid.NewV4(),
      Time: time.Now(),
      ObjectType: "{{$Type.Singular}}",
      ObjectID: input.ID,
      Number: n,
    })
    defer func() { exitIteration() }()

    for _, e := range mctx.handlers {
      h, ok := e.({{$Type.Singular}}BeforeSaveHandler)
      if !ok {
        continue
      }

      skipped := false
      forced := false

      if options != nil {
        if options.SkipCallbacks.Match("{{$Type.Singular}}", h.GetName(), input.ID) {
          skipped = true
        }
        if options.ForceCallbacks.Match("{{$Type.Singular}}", h.GetName(), input.ID) {
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
        ID: uuid.NewV4(),
        Time: before,
        Name: h.GetQualifiedName(),
        Skipped: skipped,
        Forced: forced,
        Triggered: triggered.Changes(&c, input),
      })
      defer func() { exitCallback() }()

      a := *input

      if !skipped || forced {
        if err := h.Func(ctx, tx, uid, euid, options, &c, input); err != nil {
          return nil, errors.Wrapf(err, "{{$Type.Singular}}APISave: BeforeSave callback %s for %s failed", h.Name, input.ID)
        }
      }

      traceregistry.Add(ctx, traceregistry.EventCallbackComplete{
        ID: uuid.NewV4(),
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
{{- if not $Field.IgnoreInput}}
  if !Compare(input.{{$Field.GoName}}, p.{{$Field.GoName}}) {{if $Field.Masked}}&& !Compare(input.{{$Field.GoName}}, strings.Repeat("*", len(input.{{$Field.GoName}}))){{end}} {
    skip = false

{{- if $Field.Enum}}
{{- if $Field.Array}}
    for i, v := range input.{{$Field.GoName}} {
      if !{{$Type.Singular}}Valid{{$Field.GoName}}[v] {
        return nil, errors.Errorf("{{$Type.Singular}}APISave: value for member %d of field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", i, {{$Type.Singular}}Values{{$Field.GoName}}, input.{{$Field.GoName}})
      }
    }
{{- else}}
    if !{{$Type.Singular}}Valid{{$Field.GoName}}[input.{{$Field.GoName}}] {
      return nil, errors.Errorf("{{$Type.Singular}}APISave: value for field \"{{$Field.APIName}}\" was incorrect; expected one of %v but got %q", {{$Type.Singular}}Values{{$Field.GoName}}, input.{{$Field.GoName}})
    }
{{- end}}
{{- end}}

    uc[{{$Type.Singular}}Table{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(input.{{$Field.GoName}}){{else}}input.{{$Field.GoName}}{{end}})
{{- if $Type.HasAudit}}
    changed["{{$Field.GoName}}"] = []interface{}{p.{{$Field.GoName}}, input.{{$Field.GoName}}}
{{- end}}
  }
{{- end}}
{{- end}}

  if skip {
    return input, nil
  }

{{if $Type.HasVersion -}}
  input.Version = input.Version + 1
  uc[{{$Type.Singular}}TableVersion] = sqlbuilder.Bind(input.Version)
{{- if $Type.HasAudit}}
  if !Compare(input.Version, p.Version) {
    changed["Version"] = []interface{}{p.Version, input.Version}
  }
{{- end}}
{{- end}}
{{if $Type.HasUpdatedAt -}}
  input.UpdatedAt = now
  uc[{{$Type.Singular}}TableUpdatedAt] = sqlbuilder.Bind(input.UpdatedAt)
{{- if $Type.HasAudit}}
  if !Compare(input.UpdatedAt, p.UpdatedAt) {
    changed["UpdatedAt"] = []interface{}{p.UpdatedAt, input.UpdatedAt}
  }
{{- end}}
{{- end}}
{{if $Type.HasUpdaterID -}}
  input.UpdaterID = euid
  uc[{{$Type.Singular}}TableUpdaterID] = sqlbuilder.Bind(input.UpdaterID)
{{- if $Type.HasAudit}}
  if !Compare(input.UpdaterID, p.UpdaterID) {
    changed["UpdaterID"] = []interface{}{p.UpdaterID, input.UpdaterID}
  }
{{- end}}
{{- end}}

  qb := sqlbuilder.Update().Table({{$Type.Singular}}Table).Set(uc).Where(sqlbuilder.Eq({{$Type.Singular}}TableID, sqlbuilder.Bind(input.ID)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APISave: couldn't generate query")
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APISave: couldn't update record")
  }

{{if $Type.HasVersion}}
  if _, err := tx.ExecContext(ctx, "select pg_notify('model_changes', $1)", fmt.Sprintf("{{$Type.Singular}}/%s/%d", input.ID, input.Version)); err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APISave: couldn't send postgres notification")
  }
{{end}}

  changeregistry.Add(ctx, "{{$Type.Singular}}", input.ID)

{{if $Type.HasAudit}}
  if err := RecordAuditEvent(ctx, tx, uuid.NewV4(), time.Now(), uid, euid, "update", "{{$Type.Singular}}", input.ID, changed); err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APISave: couldn't create audit record")
  }
{{end}}

  return input, nil
}

func (mctx *ModelContext) {{$Type.Singular}}APIHandleSave(rw http.ResponseWriter, r *http.Request, db *sql.DB, uid, euid uuid.UUID) {
  var input {{$Type.Singular}}

  switch r.Header.Get("content-type") {
  case "application/msgpack":
    if err := msgpack.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  default:
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  }

  tx, err := db.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable})
  if err != nil {
    panic(err)
  }
  defer tx.Rollback()

  if _, err := tx.ExecContext(r.Context(), "set constraints all deferred"); err != nil {
    panic(err)
  }

  options, err := APIOptionsFromRequest(r)
  if err != nil {
    panic(err)
  }

  v, err := mctx.{{$Type.Singular}}APISave(r.Context(), tx, uid, euid, time.Now(), &input, options)
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
      v, err := Find(k, mctx, r.Context(), tx, id, &uid, &euid)
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

  a := accept.Parse(r.Header.Get("accept"))

  f, _ := a.Negotiate("application/json", "application/msgpack")

  switch f {
  case "application/msgpack":
    rw.Header().Set("content-type", "application/msgpack")
    rw.WriteHeader(http.StatusOK)

    if err := msgpack.NewEncoder(rw).Encode(result); err != nil {
      panic(err)
    }
  default:
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
}

func (mctx *ModelContext) {{$Type.Singular}}APIHandleSaveMultiple(rw http.ResponseWriter, r *http.Request, db *sql.DB, uid, euid uuid.UUID) {
  var input struct { Records []{{$Type.Singular}} "json:\"records\"" }
  var output struct {
    Time time.Time "json:\"time\""
    Records []{{$Type.Singular}} "json:\"records\""
    Changed map[string][]interface{} "json:\"changed\""
  }

  switch r.Header.Get("content-type") {
  case "application/msgpack":
    if err := msgpack.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  default:
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
      panic(err)
    }
  }

  tx, err := db.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable})
  if err != nil {
    panic(err)
  }
  defer tx.Rollback()

  if _, err := tx.ExecContext(r.Context(), "set constraints all deferred"); err != nil {
    panic(err)
  }

  options, err := APIOptionsFromRequest(r)
  if err != nil {
    panic(err)
  }

  for i := range input.Records {
    v, err := mctx.{{$Type.Singular}}APISave(r.Context(), tx, uid, euid, time.Now(), &input.Records[i], options)
    if err != nil {
      panic(err)
    }

    output.Records = append(output.Records, *v)
  }

  output.Time = time.Now()
  output.Changed = make(map[string][]interface{})

  for k, l := range changeregistry.ChangesFromRequest(r) {
    for _, id := range l {
      v, err := Find(k, mctx, r.Context(), tx, id, &uid, &euid)
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

  a := accept.Parse(r.Header.Get("accept"))

  f, _ := a.Negotiate("application/json", "application/msgpack")

  switch f {
  case "application/msgpack":
    rw.Header().Set("content-type", "application/msgpack")
    rw.WriteHeader(http.StatusOK)

    if err := msgpack.NewEncoder(rw).Encode(output); err != nil {
      panic(err)
    }
  default:
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
}
{{end}}
`))
