package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"go/types"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/grsmv/inflect"
	"github.com/pkg/errors"

	"fknsrs.biz/p/apitypes"
)

type APIWriter struct{ Dir string }

func NewAPIWriter(dir string) *APIWriter { return &APIWriter{Dir: dir} }

func (APIWriter) Name() string     { return "api" }
func (APIWriter) Language() string { return "go" }
func (w APIWriter) File(typeName string) string {
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
		"gopkg.in/vmihailenco/msgpack.v2",
		"movingdata.com/p/wbi/internal/apihelpers",
		"movingdata.com/p/wbi/internal/apitypes",
		"movingdata.com/p/wbi/internal/cookiesession",
		"movingdata.com/p/wbi/internal/trace",
	}
}

var apiJSTypes = map[string]string{
	"string":          "string",
	"int":             "number",
	"int64":           "number",
	"float64":         "number",
	"bool":            "boolean",
	"uuid.UUID":       "string",
	"decimal.Decimal": "string",
	"time.Time":       "string",
	"civil.Date":      "string",
	"json.RawMessage": "any",
}

var apiIgnoreInput = map[string]bool{
	"createdAt": true,
	"updatedAt": true,
	"creatorId": true,
	"updaterId": true,
}

func (w *APIWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	r, err := regexp.Compile("[A-Z]+[a-z]+")
	if err != nil {
		return err
	}

	words := r.FindAllString(strings.ToUpper(typeName[0:1])+typeName[1:], -1)
	if len(words) == 0 {
		words = []string{strings.ToUpper(typeName[0:1]) + typeName[1:]}
	}
	words[len(words)-1] = inflect.Pluralize(words[len(words)-1])

	var (
		lowerPlural    = uclcc.String(inflect.Camelize(strings.Join(words, "_")))
		fields         []apitypes.Field
		specialOrders  []apitypes.SpecialOrder
		specialFilters []apitypes.Filter
		canCreate      = false
		canUpdate      = false
		canHide        = false
		hasCreatedAt   = false
		hasUpdatedAt   = false
		hasCreatorID   = false
		hasUpdaterID   = false
		hasAudit       = true
		hasUserFilter  = false
		httpSearch     = true
		httpGet        = true
		httpCreate     = true
		httpUpdate     = true
	)

	for i := 0; i < structType.NumFields(); i++ {
		f := structType.Field(i)

		jsonName, _ := getAndParseTagIndex(structType, i, "json")
		if jsonName == "-" {
			jsonName = ""
		}

		apiName, apiTagOptions := getAndParseTagIndex(structType, i, "api")
		if apiName == "-" {
			continue
		} else if apiName == "" {
			if jsonName != "" {
				apiName = jsonName
			} else {
				apiName = uclc.String(f.Name())
			}
		}

		sqlName, _ := getAndParseTagIndex(structType, i, "sql")
		if sqlName == "" {
			sqlName = ucls.String(f.Name())
		}

		if apiName == "createdAt" {
			hasCreatedAt = true
			canCreate = true
		}
		if apiName == "updatedAt" {
			hasUpdatedAt = true
			canUpdate = true
		}
		if apiName == "creatorId" {
			hasCreatorID = true
		}
		if apiName == "updaterId" {
			hasUpdaterID = true
		}

		if a, ok := apiTagOptions["lowerPlural"]; ok && len(a) > 0 && len(a[0]) > 0 {
			lowerPlural = a[0][0]
		}

		if _, ok := apiTagOptions["noaudit"]; ok {
			hasAudit = false
		}

		if _, ok := apiTagOptions["nosearch"]; ok {
			httpSearch = false
		}
		if _, ok := apiTagOptions["noget"]; ok {
			httpGet = false
		}
		if _, ok := apiTagOptions["nocreate"]; ok {
			httpCreate = false
		}
		if _, ok := apiTagOptions["noupdate"]; ok {
			httpUpdate = false
		}

		var enums apitypes.Enums
		if s := getTagIndex(structType, i, "enum"); s != "" {
			a := strings.Split(s[1:], string(s[0]))

			enums = make(apitypes.Enums, len(a))

			for i, s := range a {
				b := strings.SplitN(s, ":", 3)
				if len(b) == 1 {
					b = append(b, strings.Title(strings.Replace(b[0], "-", " ", -1)))
				}
				if len(b) == 2 {
					v := b[0]
					if v == "" {
						v = "empty"
					}

					b = append(b, lkucc.String(v))
				}

				enums[i] = apitypes.Enum{b[0], b[1], b[2]}
			}
		}

		ft := f.Type()

		isSlice := false
		if p, ok := ft.(*types.Slice); ok {
			isSlice = true
			ft = p.Elem()
		}

		isPointer := false
		if p, ok := ft.(*types.Pointer); ok {
			isPointer = true
			ft = p.Elem()
		}

		_, noOrder := apiTagOptions["noorder"]

		_, omitEmpty := apiTagOptions["omitempty"]

		if omitEmpty && len(enums) > 0 {
			found := false

			for _, e := range enums {
				if e.Value == "" {
					found = true
					break
				}
			}

			if !found {
				return errors.Errorf("can't specify omitEmpty with enum unless one is an empty string; field=%v.%v", namedType.String(), f.Name())
			}
		}

		var sequence, sequencePrefix string
		for _, opt := range apiTagOptions["sequence"] {
			switch len(opt) {
			case 1:
				sequence = opt[0]
				sequencePrefix = ""
			case 2:
				sequence = opt[0]
				sequencePrefix = opt[1]
			default:
				return errors.Errorf("sequence option needs exactly one or two parameters")
			}
		}

		gf := apitypes.Field{
			GoName:         f.Name(),
			APIName:        apiName,
			IgnoreInput:    apiIgnoreInput[apiName],
			NoOrder:        noOrder,
			OmitEmpty:      omitEmpty,
			Enum:           enums,
			Sequence:       sequence,
			SequencePrefix: sequencePrefix,
		}

		var goType, jsType, jsonType string

		switch ft := ft.(type) {
		case *types.Basic:
			goType = ft.String()
			jsType = apiJSTypes[ft.String()]
			jsonType = apiJSTypes[ft.String()]
		case *types.Named:
			goType = ft.Obj().Pkg().Name() + "." + ft.Obj().Name()
			jsType = apiJSTypes[ft.Obj().Pkg().Name()+"."+ft.Obj().Name()]
			jsonType = apiJSTypes[ft.Obj().Pkg().Name()+"."+ft.Obj().Name()]
		default:
			return errors.Errorf("unrecognised field type %s", ft.String())
		}

		if goType == "" {
			return errors.Errorf("couldn't determine go type for %s", ft)
		}
		if jsType == "" {
			return errors.Errorf("couldn't determine js type for %s", ft)
		}

		var jsEnums []string
		var jsonEnums []string

		if len(enums) > 0 {
			switch jsType {
			case "string":
				jsEnums = make([]string, len(enums))
				jsonEnums = make([]string, len(enums))
				for i := range enums {
					jsEnums[i] = "'" + enums[i].Value + "'"
					jsonEnums[i] = enums[i].Value
				}
			case "number":
				jsEnums = make([]string, len(enums))
				jsonEnums = make([]string, len(enums))
				for i := range enums {
					jsEnums[i] = enums[i].Value
					jsonEnums[i] = enums[i].Value
				}
			default:
				return errors.Errorf("got enum values for %q but can't make js type for %q", f.Name(), jsType)
			}
		}

		if len(jsEnums) > 0 {
			jsType = typeName + gf.GoName
		}

		gf.GoType = goType
		gf.JSType = jsType
		gf.JSONType = map[string]interface{}{"type": jsonType}

		if len(jsonEnums) > 0 {
			gf.JSONType["enum"] = jsonEnums
		}

		if isPointer {
			gf.GoType = "*" + gf.GoType
			gf.JSType = "?" + gf.JSType
			gf.IsNull = true
		}
		if isSlice {
			gf.GoType = "[]" + gf.GoType
			gf.JSType = "$ReadOnlyArray<" + gf.JSType + ">"
			gf.Array = true
			gf.JSONType = map[string]interface{}{
				"type":  "array",
				"items": gf.JSONType,
			}
		}

		for range apiTagOptions["userFilter"] {
			hasUserFilter = true
		}

		if a := apiTagOptions["userMask"]; len(a) > 0 {
			if len(a[0]) >= 1 {
				gf.UserMask = typeName + "UserMask" + a[0][0]
			} else {
				gf.UserMask = typeName + "UserMask" + f.Name()
			}

			if len(a[0]) >= 2 {
				gf.UserMaskValue = typeName + "UserMaskValue" + a[0][1]
			} else {
				gf.UserMaskValue = "sharedMask_"
				if gf.IsNull {
					gf.UserMaskValue += "null_"
				}
				if gf.Array {
					gf.UserMaskValue += "array_"
				}
				gf.UserMaskValue += strings.Replace(goType, ".", "_", -1)
			}
		}

		for _, opts := range apiTagOptions["specialOrder"] {
			o := apitypes.SpecialOrder{APIName: apiName, GoName: f.Name()}

			if len(opts) > 0 && opts[0] != "" {
				o.APIName = opts[0]
			}
			if len(opts) > 1 && opts[1] != "" {
				o.GoName = opts[1]
			}

			specialOrders = append(specialOrders, o)
		}

		for _, opts := range apiTagOptions["specialFilter"] {
			goName := lcucc.String(apiName)
			if len(opts) > 0 && opts[0] != "" {
				goName = opts[0]
			}

			name := apiName
			if len(opts) > 1 && opts[1] != "" {
				name = opts[1]
			}

			f := apitypes.Filter{
				Name:         name,
				GoName:       goName,
				JSType:       jsType,
				JSONType:     jsonType,
				TestOperator: "typeof",
				TestType:     jsType,
			}

			switch ft := ft.(type) {
			case *types.Basic:
				f.GoType = "*" + ft.String()
				f.TestType = apiJSTypes[ft.String()]
			case *types.Named:
				f.GoType = "*" + ft.Obj().Pkg().Name() + "." + ft.Obj().Name()
				f.TestType = apiJSTypes[ft.Obj().Pkg().Name()+"."+ft.Obj().Name()]
			}

			specialFilters = append(specialFilters, f)
		}

		filterOptions := apiTagOptions["filter"]

		if len(filterOptions) == 0 || filterOptions[0][0] == "defaults" {
			var others [][]string
			if len(filterOptions) > 1 {
				others = filterOptions[1:]
			}

			switch gf.GoType {
			case "uuid.UUID", "*uuid.UUID":
				filterOptions = [][]string{{"="}, {"!="}, {"in"}, {"not_in"}}
			case "string", "*string":
				filterOptions = [][]string{{"="}, {"!="}, {"@@"}, {"contains"}, {"prefix"}}
			case "int", "*int", "float64", "*float64", "decimal.Decimal", "*decimal.Decimal":
				filterOptions = [][]string{{"="}, {"!="}, {"<"}, {"<="}, {">"}, {">="}}
			case "[]uuid.UUID", "[]string", "[]int", "[]decimal.Decimal":
				filterOptions = [][]string{{"@>"}, {"!@>"}, {"<@"}, {"!<@"}, {"&&"}, {"!&&"}}
			case "bool", "*bool":
				filterOptions = [][]string{{"="}, {"!="}}
			case "time.Time", "*time.Time", "civil.Date", "*civil.Date":
				filterOptions = [][]string{{"="}, {"!="}, {"<"}, {"<="}, {">"}, {">="}}
			}

			if isPointer {
				filterOptions = append(filterOptions, []string{"is_null"}, []string{"is_not_null"})
			}

			if len(jsonEnums) > 0 {
				filterOptions = append(filterOptions, []string{"in"}, []string{"not_in"})
			}

			filterOptions = append(filterOptions, others...)
		}

		for _, opts := range filterOptions {
			operator := "="
			if len(opts) > 0 && opts[0] != "" {
				operator = opts[0]
			}

			jsName := apiName
			goName := f.Name()
			filterJSONType := jsonType
			filterJSType := jsType
			switch operator {
			case "!=":
				jsName = jsName + "Ne"
				goName = goName + "Ne"
			case "<":
				jsName = jsName + "Lt"
				goName = goName + "Lt"
			case "<=":
				jsName = jsName + "Lte"
				goName = goName + "Lte"
			case ">":
				jsName = jsName + "Gt"
				goName = goName + "Gt"
			case ">=":
				jsName = jsName + "Gte"
				goName = goName + "Gte"
			case "in":
				jsName = jsName + "In"
				goName = goName + "In"
			case "not_in":
				jsName = jsName + "NotIn"
				goName = goName + "NotIn"
			case "is_null":
				jsName = jsName + "IsNull"
				goName = goName + "IsNull"
				filterJSONType = "boolean"
				filterJSType = "boolean"
			case "is_not_null":
				jsName = jsName + "IsNotNull"
				goName = goName + "IsNotNull"
				filterJSONType = "boolean"
				filterJSType = "boolean"
			case "@@":
				jsName = jsName + "Match"
				goName = goName + "Match"
			case "contains":
				jsName = jsName + "Contains"
				goName = goName + "Contains"
			case "prefix":
				jsName = jsName + "StartsWith"
				goName = goName + "StartsWith"
			case "@>":
				jsName = jsName + "SupersetOf"
				goName = goName + "SupersetOf"
			case "!@>":
				jsName = jsName + "NotSupersetOf"
				goName = goName + "NotSupersetOf"
			case "<@":
				jsName = jsName + "SubsetOf"
				goName = goName + "SubsetOf"
			case "!<@":
				jsName = jsName + "NotSubsetOf"
				goName = goName + "NotSubsetOf"
			case "&&":
				jsName = jsName + "Intersects"
				goName = goName + "Intersects"
			case "!&&":
				jsName = jsName + "NotIntersects"
				goName = goName + "NotIntersects"
			}
			if len(opts) > 1 && opts[1] != "" {
				jsName = opts[1]
				goName = opts[1]
			}

			gff := apitypes.Filter{
				Operator:     operator,
				Name:         jsName,
				GoName:       goName,
				JSType:       filterJSType,
				JSONType:     filterJSONType,
				TestOperator: "typeof",
				TestType:     filterJSType,
			}

			switch ft := ft.(type) {
			case *types.Basic:
				gff.GoType = "*" + ft.String()
				gff.TestType = apiJSTypes[ft.String()]
			case *types.Named:
				gff.GoType = "*" + ft.Obj().Pkg().Name() + "." + ft.Obj().Name()
				gff.TestType = apiJSTypes[ft.Obj().Pkg().Name()+"."+ft.Obj().Name()]
			}

			switch operator {
			case "is_null", "is_not_null":
				gff.GoType = "*bool"
				gff.JSType = "boolean"
			case "in", "not_in", "@>", "!@>", "<@", "!<@", "&&", "!&&":
				gff.GoType = "[]" + strings.TrimPrefix(gff.GoType, "*")
				gff.JSType = "$ReadOnlyArray<" + strings.TrimPrefix(gff.JSType, "?") + ">"
				gff.TestOperator = "is_array"
			}

			gf.Filters = append(gf.Filters, gff)
		}

		if f.Exported() {
			fields = append(fields, gf)
		}
	}

	return apiTemplate.Execute(wr, apitypes.Model{
		HTTPSearch:     httpSearch,
		HTTPGet:        httpGet,
		HTTPCreate:     httpCreate,
		HTTPUpdate:     httpUpdate,
		Singular:       typeName,
		Plural:         inflect.Camelize(strings.Join(words, "_")),
		LowerPlural:    lowerPlural,
		Fields:         fields,
		SpecialOrders:  specialOrders,
		SpecialFilters: specialFilters,
		CanCreate:      canCreate,
		CanUpdate:      canUpdate,
		CanHide:        canHide,
		HasCreatedAt:   hasCreatedAt,
		HasUpdatedAt:   hasUpdatedAt,
		HasCreatorID:   hasCreatorID,
		HasUpdaterID:   hasUpdaterID,
		HasAudit:       hasAudit,
		HasUserFilter:  hasUserFilter,
	})
}

func formatGo(v interface{}) string {
	if v == nil {
		return "nil"
	}

	switch v := v.(type) {
	case map[string]interface{}:
		var keys []string
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var arr []string
		for _, k := range keys {
			arr = append(arr, formatGo(k)+": "+formatGo(v[k])+",\n")
		}

		return fmt.Sprintf("map[string]interface{}{\n%s}", strings.Join(arr, ""))
	default:
		vv := reflect.ValueOf(v)
		t := vv.Type()

		switch vv.Kind() {
		case reflect.Slice:
			tt := t.Elem()

			var arr []string
			for i := 0; i < vv.Len(); i++ {
				arr = append(arr, formatGo(vv.Index(i).Interface())+",\n")
			}

			return fmt.Sprintf("[]%s{\n%s}", tt.String(), strings.Join(arr, ""))
		case reflect.Struct:
			var arr []string
			for i := 0; i < t.NumField(); i++ {
				tf := t.Field(i)
				vf := vv.Field(i)

				if !vf.CanInterface() {
					continue
				}

				if reflect.DeepEqual(vf.Interface(), reflect.Zero(vf.Type()).Interface()) {
					continue
				}

				arr = append(arr, tf.Name+": "+formatGo(vf.Interface())+",\n")
			}

			return fmt.Sprintf("%s{\n%s}", t.String(), strings.Join(arr, ""))
		default:
			return fmt.Sprintf("%#v", v)
		}
	}
}

var apiTemplateFunctions = template.FuncMap{
	"Hash": func(a ...string) string {
		h := sha256.New()
		for _, e := range a {
			_, _ = h.Write([]byte(e))
		}
		v := base64.StdEncoding.EncodeToString(h.Sum(nil))
		return v[0:6]
	},
	"LKUCC": lkucc.String,
	"UCLS":  ucls.String,
	"Dump":  formatGo,
	"Default": func(defaultValue, input string) string {
		if input == "" {
			return defaultValue
		}

		return input
	},
}

var apiTemplate = template.Must(template.New("apiTemplate").Funcs(apiTemplateFunctions).Parse(`
{{$Type := .}}

var {{$Type.Singular}}APIType = {{Dump $Type}}

func init() {
  registerSwaggerSchema({{$Type.Singular}}APIType)
}

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
  {{$Filter.GoName}} {{$Filter.GoType}} "schema:\"{{$Filter.Name}}\" json:\"{{$Filter.Name}},omitempty\""
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

  var a []sqlbuilder.AsExpr

{{range $Field := $Type.Fields}}
{{- range $Filter := $Field.Filters}}
{{- if eq $Filter.Operator "is_null"}}
    if !isNil(p.{{$Filter.GoName}}) {
      if truthy(*p.{{$Filter.GoName}}) {
        a = append(a, sqlbuilder.IsNull({{$Type.Singular}}Table{{$Field.GoName}}))
      } else {
        a = append(a, sqlbuilder.IsNotNull({{$Type.Singular}}Table{{$Field.GoName}}))
      }
    }
{{- else if eq $Filter.Operator "is_not_null"}}
    if !isNil(p.{{$Filter.GoName}}) {
      if truthy(*p.{{$Filter.GoName}}) {
        a = append(a, sqlbuilder.IsNotNull({{$Type.Singular}}Table{{$Field.GoName}}))
      } else {
        a = append(a, sqlbuilder.IsNull({{$Type.Singular}}Table{{$Field.GoName}}))
      }
    }
{{- else if eq $Filter.Operator "in"}}
    if p.{{$Filter.GoName}} != nil {
      l := make([]sqlbuilder.AsExpr, len(p.{{$Filter.GoName}}))
      for i := range p.{{$Filter.GoName}} {
        l[i] = sqlbuilder.Bind(p.{{$Filter.GoName}}[i])
      }

      if len(l) == 0 {
        a = append(a, sqlbuilder.Literal("1 = 0"))
      } else {
        a = append(a, sqlbuilder.In({{$Type.Singular}}Table{{$Field.GoName}}, l...))
      }
    }
{{- else if eq $Filter.Operator "not_in"}}
    if p.{{$Filter.GoName}} != nil {
      l := make([]sqlbuilder.AsExpr, len(p.{{$Filter.GoName}}))
      for i := range p.{{$Filter.GoName}} {
        l[i] = sqlbuilder.Bind(p.{{$Filter.GoName}}[i])
      }

      if len(l) == 0 {
        a = append(a, sqlbuilder.Literal("1 = 1"))
      } else {
        a = append(a, sqlbuilder.NotIn({{$Type.Singular}}Table{{$Field.GoName}}, l...))
      }
    }
{{- else if eq $Filter.Operator "contains"}}
    if !isNil(p.{{$Filter.GoName}}) {
      a = append(a, sqlbuilder.BinaryOperator("ilike", {{$Type.Singular}}Table{{$Field.GoName}}, sqlbuilder.Bind("%" + *p.{{$Filter.GoName}} + "%")))
    }
{{- else if eq $Filter.Operator "prefix"}}
    if !isNil(p.{{$Filter.GoName}}) {
      a = append(a, sqlbuilder.BinaryOperator("ilike", {{$Type.Singular}}Table{{$Field.GoName}}, sqlbuilder.Bind(*p.{{$Filter.GoName}} + "%")))
    }
{{- else if or (eq $Filter.Operator "@>") (eq $Filter.Operator "!@>") (eq $Filter.Operator "<@") (eq $Filter.Operator "!<@") (eq $Filter.Operator "&&") (eq $Filter.Operator "!&&")}}
    if p.{{$Filter.GoName}} != nil {
      a = append(a, sqlbuilder.BinaryOperator("{{$Filter.Operator}}", {{$Type.Singular}}Table{{$Field.GoName}}, sqlbuilder.Bind(pq.Array(p.{{$Filter.GoName}}))))
    }
{{- else}}
    if !isNil(p.{{$Filter.GoName}}) {
      a = append(a, sqlbuilder.BinaryOperator("{{$Filter.Operator}}", {{$Type.Singular}}Table{{$Field.GoName}}, sqlbuilder.Bind(*p.{{$Filter.GoName}})))
    }
{{- end}}
{{- end}}
{{- end}}
{{- range $Filter := $Type.SpecialFilters}}
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
  if !isNil(p.Order) {
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

  if !isNil(p.Offset) && !isNil(p.Limit) {
    q = q.OffsetLimit(sqlbuilder.OffsetLimit(sqlbuilder.Bind(*p.Offset), sqlbuilder.Bind(*p.Limit)))
  } else if !isNil(p.Limit) {
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

func (m *{{$Type.Singular}}FieldMask) Changes(a, b *{{$Type.Singular}}) ([]trace.Change) {
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

    if v := ctx.Value(trace.Key); v != nil {
      if tl, ok := v.(*trace.Log); ok {
        tl.Add(trace.EntryIteration{
          ID: uuid.NewV4(),
          Time: time.Now(),
          ObjectType: "{{$Type.Singular}}",
          ObjectID: input.ID,
          Number: n,
        })
      }
    }

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

      a := *input

      if !skipped || forced {
        if err := h.Func(ctx, tx, uid, euid, options, &c, input); err != nil {
          return nil, errors.Wrapf(err, "{{$Type.Singular}}APICreate: BeforeSave callback %s for %s failed", h.Name, input.ID)
        }
      }

      if v := ctx.Value(trace.Key); v != nil {
        if tl, ok := v.(*trace.Log); ok {
          mm := {{$Type.Singular}}FieldMaskFrom(&a, input)

          tl.Add(trace.EntryCallback{
            ID: uuid.NewV4(),
            Time: before,
            Name: h.GetQualifiedName(),
            Duration: time.Now().Sub(before),
            Skipped: skipped,
            Forced: forced,
            Triggered: triggered.Changes(&c, input),
            Changed: mm.Changes(&a, input),
          })
        }
      }
    }
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

  v, err := mctx.{{$Type.Singular}}APIGet(ctx, tx, input.ID, &uid, &euid)
  if err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APICreate: couldn't get object after creation")
  }

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

  if err := tx.Commit(); err != nil {
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

func (mctx *ModelContext) {{$Type.Singular}}APIHandleCreateMultiple(rw http.ResponseWriter, r *http.Request, db *sql.DB, uid, euid uuid.UUID) {
  var input, output struct { Records []{{$Type.Singular}} "json:\"records\"" }

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

    if v := ctx.Value(trace.Key); v != nil {
      if tl, ok := v.(*trace.Log); ok {
        tl.Add(trace.EntryIteration{
          ID: uuid.NewV4(),
          Time: time.Now(),
          ObjectType: "{{$Type.Singular}}",
          ObjectID: input.ID,
          Number: n,
        })
      }
    }

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

      a := *input

      if !skipped || forced {
        if err := h.Func(ctx, tx, uid, euid, options, &c, input); err != nil {
          return nil, errors.Wrapf(err, "{{$Type.Singular}}APISave: BeforeSave callback %s for %s failed", h.Name, input.ID)
        }
      }

      if v := ctx.Value(trace.Key); v != nil {
        if tl, ok := v.(*trace.Log); ok {
          mm := {{$Type.Singular}}FieldMaskFrom(&a, input)

          tl.Add(trace.EntryCallback{
            ID: uuid.NewV4(),
            Time: before,
            Name: h.GetQualifiedName(),
            Duration: time.Now().Sub(before),
            Skipped: skipped,
            Forced: forced,
            Triggered: triggered.Changes(&c, input),
            Changed: mm.Changes(&a, input),
          })
        }
      }
    }
  }

  uc := sqlbuilder.UpdateColumns{}

{{if $Type.HasAudit}}
  changed := make(map[string][]interface{})
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

  qb := sqlbuilder.Update().Table({{$Type.Singular}}Table).Set(uc).Where(sqlbuilder.Eq({{$Type.Singular}}TableID, sqlbuilder.Bind(input.ID)))

  qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
  if err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APISave: couldn't generate query")
  }

  if _, err := tx.ExecContext(ctx, qs, qv...); err != nil {
    return nil, errors.Wrap(err, "{{$Type.Singular}}APISave: couldn't update record")
  }

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

  if err := tx.Commit(); err != nil {
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

func (mctx *ModelContext) {{$Type.Singular}}APIHandleSaveMultiple(rw http.ResponseWriter, r *http.Request, db *sql.DB, uid, euid uuid.UUID) {
  var input, output struct { Records []{{$Type.Singular}} "json:\"records\"" }

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
