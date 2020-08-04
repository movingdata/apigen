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

	"fknsrs.biz/p/apitypes"
	"github.com/danverbraganza/varcaser/varcaser"
	"github.com/grsmv/inflect"
	"github.com/pkg/errors"
)

type writer interface {
	Name() string
	Language() string
	File(typeName string, namedType *types.Named, structType *types.Struct) string
	Imports() []string
	Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error
}

type finisher interface {
	Finish(dry bool) error
}

var headerTemplate = template.Must(template.New("header").Parse(`package {{.PackageName}}

import (
{{range $s := .Imports}}
	"{{$s}}"
{{- end}}
)

`))

var (
	lcucc = varcaser.Caser{From: varcaser.LowerCamelCase, To: varcaser.UpperCamelCaseKeepCaps}
	uclc  = varcaser.Caser{From: varcaser.UpperCamelCase, To: varcaser.LowerCamelCase}
	uclcc = varcaser.Caser{From: varcaser.UpperCamelCase, To: varcaser.LowerCamelCaseKeepCaps}
	ucls  = varcaser.Caser{From: varcaser.UpperCamelCase, To: varcaser.LowerSnakeCase}
	lkucc = varcaser.Caser{From: varcaser.KebabCase, To: varcaser.UpperCamelCase}
)

func getFieldIndex(t *types.Struct, name string) (int, bool) {
	for i := 0; i < t.NumFields(); i++ {
		if f := t.Field(i); f.Name() == name {
			return i, true
		}
	}

	return 0, false
}

func getField(t *types.Struct, field string) *types.Var {
	if i, ok := getFieldIndex(t, field); ok {
		return t.Field(i)
	}

	return nil
}

func getTagIndex(t *types.Struct, field int, tag string) string {
	return reflect.StructTag(t.Tag(field)).Get(tag)
}

func getTag(t *types.Struct, field, tag string) string {
	if i, ok := getFieldIndex(t, field); ok {
		return getTagIndex(t, i, tag)
	}

	return ""
}

func parseTag(tag string) (string, map[string][][]string) {
	bits := strings.Split(tag, ",")

	m := make(map[string][][]string)

	if len(bits) > 1 {
		for _, e := range bits[1:] {
			if a := strings.Split(e, ":"); len(a) == 1 {
				m[a[0]] = append(m[a[0]], nil)
			} else {
				m[a[0]] = append(m[a[0]], a[1:])
			}
		}
	}

	return bits[0], m
}

func getAndParseTag(t *types.Struct, field, tag string) (string, map[string][][]string) {
	return parseTag(getTag(t, field, tag))
}

func getAndParseTagIndex(t *types.Struct, field int, tag string) (string, map[string][][]string) {
	return parseTag(getTagIndex(t, field, tag))
}

var (
	wordRegexp = regexp.MustCompile("[A-Z]+[a-z]+")
)

func pluralFor(s string) (string, string) {
	words := wordRegexp.FindAllString(strings.ToUpper(s[0:1])+s[1:], -1)

	if len(words) == 0 {
		words = []string{s}
	}

	pluralWords := words[:]
	pluralWords[len(pluralWords)-1] = inflect.Pluralize(pluralWords[len(pluralWords)-1])
	pluralSnake := strings.ToLower(strings.Join(pluralWords, "_"))
	pluralCamel := strings.Join(pluralWords, "")

	return pluralSnake, pluralCamel
}

func singularFor(s string) (string, string) {
	words := wordRegexp.FindAllString(strings.ToUpper(s[0:1])+s[1:], -1)

	if len(words) == 0 {
		words = []string{s}
	}

	singularWords := words[:]
	singularWords[len(singularWords)-1] = inflect.Singularize(singularWords[len(singularWords)-1])
	singularSnake := strings.ToLower(strings.Join(singularWords, "_"))
	singularCamel := strings.Join(singularWords, "")

	return singularSnake, singularCamel
}

var jsTypes = map[string]string{
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

var ignoreInput = map[string]bool{
	"version":   true,
	"createdAt": true,
	"updatedAt": true,
	"creatorId": true,
	"updaterId": true,
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

var tplFunc = template.FuncMap{
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

func makeModel(typeName string, namedType *types.Named, structType *types.Struct) (*apitypes.Model, error) {
	r, err := regexp.Compile("[A-Z]+[a-z]+")
	if err != nil {
		return nil, err
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
		hasVersion     = false
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

		if apiName == "version" {
			hasVersion = true
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
				return nil, errors.Errorf("can't specify omitEmpty with enum unless one is an empty string; field=%v.%v", namedType.String(), f.Name())
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
				return nil, errors.Errorf("sequence option needs exactly one or two parameters")
			}
		}

		gf := apitypes.Field{
			GoName:         f.Name(),
			APIName:        apiName,
			SQLName:        sqlName,
			IgnoreInput:    ignoreInput[apiName],
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
			jsType = jsTypes[ft.String()]
			jsonType = jsTypes[ft.String()]
		case *types.Named:
			goType = ft.Obj().Pkg().Name() + "." + ft.Obj().Name()
			jsType = jsTypes[ft.Obj().Pkg().Name()+"."+ft.Obj().Name()]
			jsonType = jsTypes[ft.Obj().Pkg().Name()+"."+ft.Obj().Name()]
		default:
			return nil, errors.Errorf("unrecognised field type %s", ft.String())
		}

		if goType == "" {
			return nil, errors.Errorf("couldn't determine go type for %s", ft)
		}
		if jsType == "" {
			return nil, errors.Errorf("couldn't determine js type for %s", ft)
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
				return nil, errors.Errorf("got enum values for %q but can't make js type for %q", f.Name(), jsType)
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
				f.TestType = jsTypes[ft.String()]
			case *types.Named:
				f.GoType = "*" + ft.Obj().Pkg().Name() + "." + ft.Obj().Name()
				f.TestType = jsTypes[ft.Obj().Pkg().Name()+"."+ft.Obj().Name()]
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
				gff.TestType = jsTypes[ft.String()]
			case *types.Named:
				gff.GoType = "*" + ft.Obj().Pkg().Name() + "." + ft.Obj().Name()
				gff.TestType = jsTypes[ft.Obj().Pkg().Name()+"."+ft.Obj().Name()]
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

	return &apitypes.Model{
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
		HasVersion:     hasVersion,
		HasCreatedAt:   hasCreatedAt,
		HasUpdatedAt:   hasUpdatedAt,
		HasCreatorID:   hasCreatorID,
		HasUpdaterID:   hasUpdaterID,
		HasAudit:       hasAudit,
		HasUserFilter:  hasUserFilter,
	}, nil
}
