package main

import (
	"crypto/sha256"
	"encoding/base64"
	"go/types"
	"io"
	"regexp"
	"strings"
	"text/template"

	"github.com/grsmv/inflect"
	"github.com/pkg/errors"

	"fknsrs.biz/p/apitypes"
)

type JSWriter struct{ Dir string }

func NewJSWriter(dir string) *JSWriter { return &JSWriter{Dir: dir} }

func (JSWriter) Name() string     { return "js" }
func (JSWriter) Language() string { return "js" }
func (w JSWriter) File(typeName string) string {
	r, err := regexp.Compile("[A-Z]+[a-z]+")
	if err != nil {
		panic(err)
	}

	words := r.FindAllString(strings.ToUpper(typeName[0:1])+typeName[1:], -1)
	if len(words) == 0 {
		words = []string{strings.ToUpper(typeName[0:1]) + typeName[1:]}
	}
	words[len(words)-1] = inflect.Pluralize(words[len(words)-1])

	return w.Dir + "/" + uclcc.String(inflect.Camelize(strings.Join(words, "_"))) + ".js"
}

func (JSWriter) Imports() []string {
	return []string{}
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

var jsIgnoreInput = map[string]bool{
	"createdAt": true,
	"updatedAt": true,
	"creatorId": true,
	"updaterId": true,
}

func (w *JSWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
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
			canCreate = true
		}
		if apiName == "updatedAt" {
			canUpdate = true
		}

		if a, ok := apiTagOptions["lowerPlural"]; ok && len(a) > 0 && len(a[0]) > 0 {
			lowerPlural = a[0][0]
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

		gf := apitypes.Field{
			GoName:      f.Name(),
			APIName:     apiName,
			IgnoreInput: jsIgnoreInput[apiName],
			OmitEmpty:   omitEmpty,
			Enum:        enums,
		}

		var goType, jsType string

		switch ft := ft.(type) {
		case *types.Basic:
			goType = ft.String()
			jsType = jsTypes[ft.String()]
		case *types.Named:
			goType = ft.Obj().Pkg().Name() + "." + ft.Obj().Name()
			jsType = jsTypes[ft.Obj().Pkg().Name()+"."+ft.Obj().Name()]
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

		if isPointer {
			gf.GoType = "*" + gf.GoType
			gf.JSType = "?" + gf.JSType
			gf.IsNull = true
		}
		if isSlice {
			gf.GoType = "[]" + gf.GoType
			gf.JSType = "$ReadOnlyArray<" + gf.JSType + ">"
			gf.Array = true
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
				Name:   name,
				GoName: goName,
				JSType: jsType,
			}

			switch ft := ft.(type) {
			case *types.Basic:
				f.GoType = "*" + ft.String()
			case *types.Named:
				f.GoType = "*" + ft.Obj().Pkg().Name() + "." + ft.Obj().Name()
			}

			specialFilters = append(specialFilters, f)
		}

		filterOptions := apiTagOptions["filter"]

		if len(filterOptions) == 0 {
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
		}

		for _, opts := range filterOptions {
			operator := "="
			if len(opts) > 0 && opts[0] != "" {
				operator = opts[0]
			}

			jsName := apiName
			goName := f.Name()
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
				filterJSType = "boolean"
			case "is_not_null":
				jsName = jsName + "IsNotNull"
				goName = goName + "IsNotNull"
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
				Name:   jsName,
				GoName: goName,
				JSType: filterJSType,
			}

			switch ft := ft.(type) {
			case *types.Basic:
				gff.GoType = "*" + ft.String()
			case *types.Named:
				gff.GoType = "*" + ft.Obj().Pkg().Name() + "." + ft.Obj().Name()
			}

			switch operator {
			case "is_null", "is_not_null":
				gff.GoType = "*bool"
				gff.JSType = "boolean"
			case "in", "not_in", "@>", "!@>", "<@", "!<@", "&&", "!&&":
				gff.GoType = "[]" + strings.TrimPrefix(gff.GoType, "*")
				gff.JSType = "$ReadOnlyArray<" + strings.TrimPrefix(gff.JSType, "?") + ">"
			}

			gf.Filters = append(gf.Filters, gff)
		}

		if f.Exported() {
			fields = append(fields, gf)
		}
	}

	return jsTemplate.Execute(wr, apitypes.Model{
		Singular:       typeName,
		LowerPlural:    lowerPlural,
		Fields:         fields,
		SpecialFilters: specialFilters,
		CanCreate:      canCreate,
		CanUpdate:      canUpdate,
	})
}

var jsTemplateFunctions = template.FuncMap{
	"Hash": func(a ...string) string {
		h := sha256.New()
		for _, e := range a {
			_, _ = h.Write([]byte(e))
		}
		v := base64.StdEncoding.EncodeToString(h.Sum(nil))
		return v[0:6]
	},
}

var jsTemplate = template.Must(template.New("jsTemplate").Funcs(jsTemplateFunctions).Parse(`
// @flow

{{$Type := .}}

import axios from 'axios';
import { useEffect } from 'react';
import { useDispatch, useSelector } from 'react-redux';
import URLSearchParams from 'url-search-params';

import {
  invalidateFetchCacheWithIDs,
  invalidateSearchCacheWithIDs,
  makeSearchKey,
  updateFetchCacheCompleteMulti,
  updateFetchCacheErrorMulti,
  updateFetchCacheLoading,
  updateFetchCachePushMulti,
  updateSearchCacheComplete,
  updateSearchCacheError,
  updateSearchCacheLoading,
} from 'lib/duckHelpers';
import type { FetchCache, SearchCache, SearchPageKey } from 'lib/duckHelpers';
import mergeArrays from 'lib/mergeArrays';
import { report } from 'lib/report';

import { errorsEnsureError } from './errors';
import type { ErrorResponse } from './errors';

{{range $Field := $Type.Fields}}
{{if $Field.Enum}}
export type {{$Type.Singular}}{{$Field.GoName}} =
{{- range $Enum := $Field.Enum}}
  | "{{$Enum.Value}}"
{{- end}}

{{range $Enum := $Field.Enum}}
export const {{$Type.LowerPlural}}Enum{{$Field.GoName}}{{$Enum.GoName}}: {{$Type.Singular}}{{$Field.GoName}} = "{{$Enum.Value}}";
{{- end}}

export const {{$Type.LowerPlural}}Values{{$Field.GoName}}: $ReadOnlyArray<{{$Type.Singular}}{{$Field.GoName}}> = [
{{- range $Enum := $Field.Enum}}
  {{$Type.LowerPlural}}Enum{{$Field.GoName}}{{$Enum.GoName}},
{{- end}}
];

export const {{$Type.LowerPlural}}Labels{{$Field.GoName}}: { [key: {{$Type.Singular}}{{$Field.GoName}}]: string } = {
{{- range $Enum := $Field.Enum}}
  [{{$Type.LowerPlural}}Enum{{$Field.GoName}}{{$Enum.GoName}}]: "{{$Enum.Label}}",
{{- end}}
}
{{- end}}
{{- end}}

const defaultPageSize = 10;

/** {{$Type.Singular}} is a complete {{$Type.Singular}} object */
export type {{$Type.Singular}} = {|
{{- range $Field := $Type.Fields}}
  {{$Field.APIName}}: {{$Field.JSType}},
{{- end}}
|};

export function generate{{$Type.Singular}}(): {{$Type.Singular}} {
  return {
{{- range $Field := $Type.Fields}}
{{- if (eq $Field.GoType "uuid.UUID")}}
    {{$Field.APIName}}: '12341234-1234-1234-1234-123412341234',
{{- else if (eq $Field.GoType "*uuid.UUID")}}
    {{$Field.APIName}}: '12341234-1234-1234-1234-123412341234',
{{- else if (eq $Field.GoType "[]uuid.UUID")}}
    {{$Field.APIName}}: ['12341234-1234-1234-1234-123412341234'],
{{- else if (eq $Field.GoType "string")}}
{{- if $Field.Enum}}
    {{$Field.APIName}}: '{{ $Field.Enum.First.Value }}',
{{- else}}
    {{$Field.APIName}}: 'test data',
{{- end}}
{{- else if (eq $Field.GoType "*string")}}
{{- if $Field.Enum}}
    {{$Field.APIName}}: '{{ $Field.Enum.First.Value }}',
{{- else}}
    {{$Field.APIName}}: 'test data',
{{- end}}
{{- else if (eq $Field.GoType "[]string")}}
{{- if $Field.Enum}}
    {{$Field.APIName}}: ['{{ $Field.Enum.First.Value }}'],
{{- else}}
    {{$Field.APIName}}: ['test data'],
{{- end}}
{{- else if (eq $Field.GoType "bool")}}
    {{$Field.APIName}}: true,
{{- else if (eq $Field.GoType "*bool")}}
    {{$Field.APIName}}: true,
{{- else if (eq $Field.GoType "int")}}
    {{$Field.APIName}}: 1000032,
{{- else if (eq $Field.GoType "*int")}}
    {{$Field.APIName}}: 1000032,
{{- else if (eq $Field.GoType "[]int")}}
    {{$Field.APIName}}: [1000032],
{{- else if (eq $Field.GoType "int64")}}
    {{$Field.APIName}}: 1000064,
{{- else if (eq $Field.GoType "*int64")}}
    {{$Field.APIName}}: 1000064,
{{- else if (eq $Field.GoType "[]int64")}}
    {{$Field.APIName}}: [1000064],
{{- else if (eq $Field.GoType "float64")}}
    {{$Field.APIName}}: 1000064.64,
{{- else if (eq $Field.GoType "*float64")}}
    {{$Field.APIName}}: 1000064.64,
{{- else if (eq $Field.GoType "time.Time")}}
    {{$Field.APIName}}: '2019-01-01T00:00:00.000Z',
{{- else if (eq $Field.GoType "*time.Time")}}
    {{$Field.APIName}}: '2019-01-01T00:00:00.000Z',
{{- else if (eq $Field.GoType "civil.Date")}}
    {{$Field.APIName}}: '2019-01-01',
{{- else if (eq $Field.GoType "*civil.Date")}}
    {{$Field.APIName}}: '2019-01-01',
{{- else if $Field.Array}}
    {{$Field.APIName}}: ['UNHANDLED_TYPE {{$Field.GoType}}'],
{{- else}}
    {{$Field.APIName}}: 'UNHANDLED_TYPE {{$Field.GoType}}',
{{- end}}
{{- end}}
  };
}

{{if $Type.CanCreate}}
/** {{$Type.Singular}}CreateInput is the data needed to call {{$Type.LowerPlural}}Create */
export type {{$Type.Singular}}CreateInput = {|
{{- range $Field := $Type.Fields}}
{{- if not (or $Field.IgnoreInput) }}
  {{$Field.APIName}}{{if $Field.OmitEmpty}}?{{end}}: {{$Field.JSType}},
{{- end}}
{{- end}}
|};
{{end}}

/** {{$Type.Singular}}SearchParams is used to call {{$Type.LowerPlural}}Search */
export type {{$Type.Singular}}SearchParams = {|
{{- range $Field := $Type.Fields}}
{{- range $Filter := $Field.Filters}}
  {{$Filter.Name}}?: {{$Filter.JSType}},
{{- end}}
{{- end}}
{{- range $Filter := $Type.SpecialFilters}}
  {{$Filter.Name}}?: {{$Filter.JSType}},
{{- end}}
  order?: string,
  pageSize?: number,
  page: SearchPageKey,
|};

export type State = {
  loading: number,
  {{$Type.LowerPlural}}: $ReadOnlyArray<{{$Type.Singular}}>,
  error: ?ErrorResponse,
  searchCache: SearchCache<{{$Type.Singular}}SearchParams>,
  fetchCache: FetchCache,
  timeouts: { [key: string]: ?TimeoutID },
};

{{if (or $Type.CanCreate $Type.CanUpdate)}}
type Invalidator = (c: SearchCache<{{$Type.Singular}}SearchParams>) => SearchCache<{{$Type.Singular}}SearchParams>;
{{end}}

{{if $Type.CanCreate}}
type {{$Type.Singular}}CreateOptions = {
  invalidate?: boolean | Invalidator,
  after?: (err: ?Error, record?: {{$Type.Singular}}) => void,
  push?: boolean,
};

type {{$Type.Singular}}CreateMultipleOptions = {
  invalidate?: boolean | Invalidator,
  after?: (err: ?Error, records?: $ReadOnlyArray<{{$Type.Singular}}>) => void,
  push?: boolean,
  timeout?: number,
};
{{end}}

{{if $Type.CanUpdate}}
type {{$Type.Singular}}UpdateOptions = {
  invalidate?: boolean | Invalidator,
  after?: (err: ?Error, record?: {{$Type.Singular}}) => void,
  push?: boolean,
  timeout?: number,
};

type {{$Type.Singular}}UpdateMultipleOptions = {
  invalidate?: boolean | Invalidator,
  after?: (err: ?Error, records?: $ReadOnlyArray<{{$Type.Singular}}>) => void,
  push?: boolean,
  timeout?: number,
};
{{end}}

export const actionCreateBegin = 'X/{{Hash $Type.LowerPlural "/CREATE_BEGIN"}}';
export const actionCreateComplete = 'X/{{Hash $Type.LowerPlural "/CREATE_COMPLETE"}}';
export const actionCreateFailed = 'X/{{Hash $Type.LowerPlural "/CREATE_FAILED"}}';
export const actionCreateMultipleBegin = 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_BEGIN"}}';
export const actionCreateMultipleComplete = 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_COMPLETE"}}';
export const actionCreateMultipleFailed = 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_FAILED"}}';
export const actionFetchBegin = 'X/{{Hash $Type.LowerPlural "/FETCH_BEGIN"}}';
export const actionFetchCompleteMulti = 'X/{{Hash $Type.LowerPlural "/FETCH_COMPLETE_MULTI"}}';
export const actionFetchFailedMulti = 'X/{{Hash $Type.LowerPlural "/FETCH_FAILED_MULTI"}}';
export const actionReset = 'X/{{Hash $Type.LowerPlural "/RESET"}}';
export const actionSearchBegin = 'X/{{Hash $Type.LowerPlural "/SEARCH_BEGIN"}}';
export const actionSearchComplete = 'X/{{Hash $Type.LowerPlural "/SEARCH_COMPLETE"}}';
export const actionSearchFailed = 'X/{{Hash $Type.LowerPlural "/SEARCH_FAILED"}}';
export const actionUpdateBegin = 'X/{{Hash $Type.LowerPlural "/UPDATE_BEGIN"}}';
export const actionUpdateCancel = 'X/{{Hash $Type.LowerPlural "/UPDATE_CANCEL"}}';
export const actionUpdateComplete = 'X/{{Hash $Type.LowerPlural "/UPDATE_COMPLETE"}}';
export const actionUpdateFailed = 'X/{{Hash $Type.LowerPlural "/UPDATE_FAILED"}}';
export const actionUpdateMultipleBegin = 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_BEGIN"}}';
export const actionUpdateMultipleCancel = 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_CANCEL"}}';
export const actionUpdateMultipleComplete = 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_COMPLETE"}}';
export const actionUpdateMultipleFailed = 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_FAILED"}}';
export const actionInvalidateCache = 'X/{{Hash $Type.LowerPlural "/INVALIDATE_CACHE"}}';
export const actionRecordPush = 'X/{{Hash $Type.LowerPlural "/RECORD_PUSH"}}';
export const actionRecordPushMulti = 'X/{{Hash $Type.LowerPlural "/RECORD_PUSH_MULTI"}}';

export type Action =
  | {
      type: 'X/{{Hash $Type.LowerPlural "/SEARCH_BEGIN"}}',
      payload: { params: {{$Type.Singular}}SearchParams, key: string, page: SearchPageKey },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/SEARCH_COMPLETE"}}',
      payload: {
        records: $ReadOnlyArray<{{$Type.Singular}}>,
        total: number,
        time: number,
        params: {{$Type.Singular}}SearchParams,
        key: string,
        page: SearchPageKey,
      },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/SEARCH_FAILED"}}',
      payload: {
        time: number,
        params: {{$Type.Singular}}SearchParams,
        key: string,
        page: SearchPageKey,
        error: ErrorResponse,
      },
    }
  | { type: 'X/{{Hash $Type.LowerPlural "/FETCH_BEGIN"}}', payload: { id: string } }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/FETCH_COMPLETE_MULTI"}}',
      payload: { ids: $ReadOnlyArray<string>, time: number, records: $ReadOnlyArray<{{$Type.Singular}}> },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/FETCH_FAILED_MULTI"}}',
      payload: { ids: $ReadOnlyArray<string>, time: number, error: ErrorResponse },
    }
{{if $Type.CanCreate}}
  | {
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_BEGIN"}}',
      payload: { record: {{$Type.Singular}} },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_COMPLETE"}}',
      payload: { record: {{$Type.Singular}}, options: {{$Type.Singular}}CreateOptions },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_FAILED"}}',
      payload: { error: ErrorResponse },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_BEGIN"}}',
      payload: { records: $ReadOnlyArray<{{$Type.Singular}}>, options: {{$Type.Singular}}CreateMultipleOptions },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_COMPLETE"}}',
      payload: { records: $ReadOnlyArray<{{$Type.Singular}}>, options: {{$Type.Singular}}CreateMultipleOptions },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_FAILED"}}',
      payload: { records: $ReadOnlyArray<{{$Type.Singular}}>, options: {{$Type.Singular}}CreateMultipleOptions, error: ErrorResponse },
    }
{{end}}
{{if $Type.CanUpdate}}
  | {
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_BEGIN"}}',
      payload: { record: {{$Type.Singular}}, timeout: number },
    }
  | { type: 'X/{{Hash $Type.LowerPlural "/UPDATE_CANCEL"}}', payload: { id: string } }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_COMPLETE"}}',
      payload: { record: {{$Type.Singular}}, options: {{$Type.Singular}}UpdateOptions },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_FAILED"}}',
      payload: { record: {{$Type.Singular}}, error: ErrorResponse },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_BEGIN"}}',
      payload: { records: $ReadOnlyArray<{{$Type.Singular}}>, timeout: number },
    }
  | { type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_CANCEL"}}', payload: { ids: string } }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_COMPLETE"}}',
      payload: { records: $ReadOnlyArray<{{$Type.Singular}}>, options: {{$Type.Singular}}UpdateMultipleOptions },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_FAILED"}}',
      payload: { records: $ReadOnlyArray<{{$Type.Singular}}>, error: ErrorResponse },
    }
{{end}}
  | { type: 'X/{{Hash $Type.LowerPlural "/RESET"}}', payload: {} }
  | { type: 'X/{{Hash $Type.LowerPlural "/INVALIDATE_CACHE"}}', payload: {} }
  | { type: 'X/{{Hash $Type.LowerPlural "/RECORD_PUSH"}}', payload: { time: number, record: {{$Type.Singular}} } }
  | { type: 'X/{{Hash $Type.LowerPlural "/RECORD_PUSH_MULTI"}}', payload: { time: number, records: $ReadOnlyArray<{{$Type.Singular}}> } }
  | { type: 'X/INVALIDATE', payload: { [key: string]: $ReadOnlyArray<string> } };

/** {{$Type.LowerPlural}}Search */
export const {{$Type.LowerPlural}}Search = (params: {{$Type.Singular}}SearchParams) => (
  dispatch: (ev: any) => void
) => {
  const p = new URLSearchParams();

  for (const k of Object.keys(params).sort()) {
    if (k === 'page' || k === 'pageSize') { continue; }

    const v: any = params[k];

    if (Array.isArray(v)) {
      p.set(k, v.slice().sort().join(','));
    } else if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') {
      p.set(k, v);
    }
  }

  let pageSize: number = defaultPageSize;
  if (typeof params.pageSize === 'number' && !Number.isNaN(params.pageSize)) {
    pageSize = params.pageSize;
  }

  if (typeof params.page === 'number' && !Number.isNaN(params.page)) {
    p.set('offset', (params.page - 1) * pageSize);
    p.set('limit', pageSize);
  }

  const key = makeSearchKey(params);

  dispatch({
    type: 'X/{{Hash $Type.LowerPlural "/SEARCH_BEGIN"}}',
    payload: { params, key, page: params.page },
  });

  axios.get('/api/{{$Type.LowerPlural}}?' + p.toString()).then(
    ({ data: { records, total, time } }: {
      data: { records: $ReadOnlyArray<{{$Type.Singular}}>, total: number, time: string },
    }) => void dispatch({
      type: 'X/{{Hash $Type.LowerPlural "/SEARCH_COMPLETE"}}',
      payload: { records, total, time: new Date(time).valueOf(), params, key, page: params.page },
    }),
    (err: Error) => {
      dispatch({
        type: 'X/{{Hash $Type.LowerPlural "/SEARCH_FAILED"}}',
        payload: {
          params,
          key,
          page: params.page,
          time: Date.now(),
          error: errorsEnsureError(err),
        },
      });
    }
  );
};

/** {{$Type.LowerPlural}}SearchIfRequired will only perform a search if the current results are older than the specified ttl, which is one minute by default */
export const {{$Type.LowerPlural}}SearchIfRequired = (
  params: {{$Type.Singular}}SearchParams,
  ttl: number = 1000 * 60,
  now: Date = new Date()
) => (dispatch: (ev: any) => void, getState: () => { {{$Type.LowerPlural}}: State }) => {
  const { {{$Type.LowerPlural}}: { searchCache } } = getState();

  const k = makeSearchKey(params);

  let refresh = false;

  const c = searchCache[k];

  if (c) {
    const { pages } = c;

    const page = pages[String(params.page)];

    if (!page) {
      refresh = true;
    } else if (page.time) {
      if (!page.loading && now.valueOf() - page.time > ttl) {
        refresh = true;
      }
    } else {
      if (!page.loading) {
        refresh = true;
      }
    }
  } else {
    refresh = true;
  }

  if (refresh) {
    dispatch({{$Type.LowerPlural}}Search(params));
  }
};

/** {{$Type.LowerPlural}}GetSearchRecords fetches the {{$Type.Singular}} objects related to a specific search query, if available */
export const {{$Type.LowerPlural}}GetSearchRecords = (
  state: State,
  params: {{$Type.Singular}}SearchParams
): ?$ReadOnlyArray<{{$Type.Singular}}> => {
  const k = makeSearchKey(params);

  const c = state.searchCache[k];
  if (!c || !c.pages) {
    return null;
  }

  const p = c.pages[String(params.page)];
  if (!p || !p.items) {
    return null;
  }

  return p.items.map(id =>
    state.{{$Type.LowerPlural}}.find(e => e.id === id)
  ).reduce((arr, e) => e ? [ ...arr, e ] : arr, ([]: $ReadOnlyArray<{{$Type.Singular}}>));
};

/** {{$Type.LowerPlural}}GetSearchMeta fetches the metadata related to a specific search query, if available */
export const {{$Type.LowerPlural}}GetSearchMeta = (
  state: State,
  params: {{$Type.Singular}}SearchParams
): ?{ time: number, total: number, loading: number } => {
  const k = makeSearchKey(params);

  const c = state.searchCache[k];
  if (!c || !c.pages) {
    return null;
  }

  const p = c.pages[String(params.page)];

  return { time: c.time, total: c.total, loading: p ? p.loading : 0 };
};

/** {{$Type.LowerPlural}}GetSearchLoading returns the loading status for a specific search query */
export const {{$Type.LowerPlural}}GetSearchLoading = (
  state: State,
  params: {{$Type.Singular}}SearchParams
): boolean => {
  const k = makeSearchKey(params);

  const c = state.searchCache[k];
  if (!c || !c.pages) {
    return false;
  }

  const p = c.pages[String(params.page)];
  if (!p) {
    return false;
  }

  return p.loading > 0;
};

/** use{{$Type.Singular}}Search forms a react hook for a specific search query */
export const use{{$Type.Singular}}Search = (params: {{$Type.Singular}}SearchParams): {
  meta: ?{ time: number, total: number, loading: number },
  loading: boolean,
  records: $ReadOnlyArray<{{$Type.Singular}}>,
} => {
  const dispatch = useDispatch();
  useEffect(() => void dispatch({{$Type.LowerPlural}}SearchIfRequired(params)));
  return useSelector(({ {{$Type.LowerPlural}} }: { {{$Type.LowerPlural}}: State }) => ({
    meta: {{$Type.LowerPlural}}GetSearchMeta({{$Type.LowerPlural}}, params),
    loading: {{$Type.LowerPlural}}GetSearchLoading({{$Type.LowerPlural}}, params) || !{{$Type.LowerPlural}}GetSearchMeta({{$Type.LowerPlural}}, params),
    records: {{$Type.LowerPlural}}GetSearchRecords({{$Type.LowerPlural}}, params) || [],
  }));
};

/** pendingFetch is a module-level metadata cache for ongoing fetch operations */ 
const pendingFetch: {
  timeout: ?TimeoutID,
  ids: $ReadOnlyArray<string>,
} = {
  timeout: null,
  ids: [],
};

function batchFetch(id: string, dispatch: (ev: any) => void) {
  if (pendingFetch.timeout === null) {
    pendingFetch.timeout = setTimeout(() => {
      const { ids } = pendingFetch;

      pendingFetch.timeout = null;
      pendingFetch.ids = [];

      axios.get('/api/{{$Type.LowerPlural}}?idIn=' + ids.join(',')).then(
        ({ data: { records }, }: { data: { records: $ReadOnlyArray<{{$Type.Singular}}> } }) => {
          dispatch({
            type: 'X/{{Hash $Type.LowerPlural "/FETCH_COMPLETE_MULTI"}}',
            payload: { ids, time: Date.now(), records },
          });
        },
        (err) => {
          report('{{$Type.LowerPlural}}Fetch failed', errorsEnsureError(err).message, errorsEnsureError(err).stack, { ids });

          dispatch({
            type: 'X/{{Hash $Type.LowerPlural "/FETCH_FAILED_MULTI"}}',
            payload: { ids, time: Date.now(), error: errorsEnsureError(err) },
          });
        },
      )
    }, 100);
  }

  if (!pendingFetch.ids.includes(id)) {
    pendingFetch.ids = pendingFetch.ids.concat([id]);

    dispatch({
      type: 'X/{{Hash $Type.LowerPlural "/FETCH_BEGIN"}}',
      payload: { id },
    });
  }
}

/** {{$Type.LowerPlural}}Fetch */
export const {{$Type.LowerPlural}}Fetch = (id: string) => (
  dispatch: (ev: any) => void
) => {
	if (typeof id !== 'string') { throw new Error('{{$Type.LowerPlural}}Fetch: id must be a string'); }
  if (!id.match(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i)) { throw new Error('{{$Type.LowerPlural}}Fetch: id must be a uuid'); }

  batchFetch(id, dispatch);
};

/** {{$Type.LowerPlural}}FetchIfRequired will only perform a fetch if the current results are older than the specified ttl, which is one minute by default */
export const {{$Type.LowerPlural}}FetchIfRequired = (
  id: string,
  ttl: number = 1000 * 60,
  now: Date = new Date()
) => (dispatch: (ev: any) => void, getState: () => { {{$Type.LowerPlural}}: State }) => {
  const { {{$Type.LowerPlural}}: { fetchCache } } = getState();

  let refresh = false;

  const c = fetchCache[id];

  if (!c) {
    refresh = true;
  } else if (c.time) {
    if (!c.loading && now.valueOf() - c.time > ttl) {
      refresh = true;
    }
  } else {
    if (!c.loading) {
      refresh = true;
    }
  }

  if (refresh) {
    dispatch({{$Type.LowerPlural}}Fetch(id));
  }
};

/** {{$Type.LowerPlural}}GetFetchMeta fetches the metadata related to a specific search query, if available */
export const {{$Type.LowerPlural}}GetFetchMeta = (state: State, id: string): ?{ time: number, loading: number } => {
  return state.fetchCache[id];
};

/** {{$Type.LowerPlural}}GetFetchLoading returns the loading status for a specific search query */
export const {{$Type.LowerPlural}}GetFetchLoading = (state: State, id: string): boolean => {
  const c = state.fetchCache[id];
  if (!c) {
    return false;
  }

  return c.loading > 0;
};

/** use{{$Type.Singular}}Fetch forms a react hook for a specific fetch query */
export const use{{$Type.Singular}}Fetch = (id: ?string): {
  loading: boolean,
  record: ?{{$Type.Singular}},
} => {
  const dispatch = useDispatch();
  useEffect(() => { if (id) { dispatch({{$Type.LowerPlural}}FetchIfRequired(id)); } });
  return useSelector(({ {{$Type.LowerPlural}} }: { {{$Type.LowerPlural}}: State }) => ({
    loading: id ? {{$Type.LowerPlural}}GetFetchLoading({{$Type.LowerPlural}}, id) : false,
    record: id ? {{$Type.LowerPlural}}.{{$Type.LowerPlural}}.find(e => e.id === id) : null,
  }));
};

{{if $Type.CanCreate}}
/** {{$Type.LowerPlural}}Create */
export const {{$Type.LowerPlural}}Create = (input: {{$Type.Singular}}CreateInput, options?: {{$Type.Singular}}CreateOptions) => (
  dispatch: (ev: any) => void
) => {
  dispatch({
    type: 'X/{{Hash $Type.LowerPlural "/CREATE_BEGIN"}}',
    payload: {},
  });

  axios.post('/api/{{$Type.LowerPlural}}', input).then(
    ({ data: record }: { data: {{$Type.Singular}} }) => {
      dispatch({
        type: 'X/{{Hash $Type.LowerPlural "/CREATE_COMPLETE"}}',
        payload: { record, options: options || {} },
      });

      if (options && options.after) {
        setImmediate(options.after, null, record);
      }
    },
    (err: Error | { response: { data: ErrorResponse } }) => {
      dispatch({
        type: 'X/{{Hash $Type.LowerPlural "/CREATE_FAILED"}}',
        payload: { error: errorsEnsureError(err) },
      });

      if (options && options.after) {
        if (err && err.response && typeof err.response.data === 'object' && err.response.data !== null) {
          setImmediate(options.after, new Error(err.response.data.message));
        } else {
          setImmediate(options.after, err);
        }
      }

      report('{{$Type.LowerPlural}}Create failed', errorsEnsureError(err).message, errorsEnsureError(err).stack, { input, options });
    }
  );
};

/** {{$Type.LowerPlural}}CreateMultiple */
export const {{$Type.LowerPlural}}CreateMultiple = (input: $ReadOnlyArray<{{$Type.Singular}}CreateInput>, options?: {{$Type.Singular}}CreateMultipleOptions) => (dispatch: (ev: any) => void) => {
  dispatch({
    type: 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_BEGIN"}}',
    payload: { records: input, options: options || {} },
  });

  axios.post('/api/{{$Type.LowerPlural}}/_multi', { records: input }).then(
    ({ data: { records } }: { data: { records: $ReadOnlyArray<{{$Type.Singular}}> } }) => {
      dispatch({
        type: 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_COMPLETE"}}',
        payload: { records, options: options || {} },
      });

      if (options && options.after) {
        setImmediate(options.after, null, records);
      }
    },
    (err: Error) => {
      dispatch({
        type: 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_FAILED"}}',
        payload: { records: input, options: options || {}, error: errorsEnsureError(err) },
      });

      if (options && options.after) {
        setImmediate(options.after, err);
      }
    }
  );
};
{{end}}

{{if $Type.CanUpdate}}
/** {{$Type.LowerPlural}}Update */
export const {{$Type.LowerPlural}}Update = (input: {{$Type.Singular}}, options?: {{$Type.Singular}}UpdateOptions) => (dispatch: (ev: any) => void, getState: () => ({ {{$Type.LowerPlural}}: State })) => {
  const previous = getState().{{$Type.LowerPlural}}.{{$Type.LowerPlural}}.find(e => e.id === input.id);
  if (!previous) {
    return;
  }

  const timeoutHandle = getState().{{$Type.LowerPlural}}.timeouts[input.id];
  if (timeoutHandle) {
    clearTimeout(timeoutHandle);
    dispatch({ type: 'X/{{Hash $Type.LowerPlural "/UPDATE_CANCEL"}}', payload: { id: input.id } });
  }

  dispatch({
    type: 'X/{{Hash $Type.LowerPlural "/UPDATE_BEGIN"}}',
    payload: {
      record: input,
      timeout: setTimeout(
        () =>
          void axios.put('/api/{{$Type.LowerPlural}}/' + input.id, input).then(
            ({ data: record }: { data: {{$Type.Singular}} }) => {
              dispatch({
                type: 'X/{{Hash $Type.LowerPlural "/UPDATE_COMPLETE"}}',
                payload: { record, options: options || {} },
              });

              if (options && options.after) {
                setImmediate(options.after, null, record);
              }
            },
            (err: Error | { response: { data: ErrorResponse } }) => {
              dispatch({
                type: 'X/{{Hash $Type.LowerPlural "/UPDATE_FAILED"}}',
                payload: { record: previous, error: errorsEnsureError(err) },
              });

              if (options && options.after) {
                if (err && err.response && typeof err.response.data === 'object' && err.response.data !== null) {
                  setImmediate(options.after, new Error(err.response.data.message));
                } else {
                  setImmediate(options.after, err);
                }
              }

              report('{{$Type.LowerPlural}}Update failed', errorsEnsureError(err).message, errorsEnsureError(err).stack, { input, options });
            }
          ),
        options && typeof options.timeout === 'number'
          ? options.timeout
          : 1000
      ),
    },
  });
};

/** {{$Type.LowerPlural}}UpdateMultiple */
export const {{$Type.LowerPlural}}UpdateMultiple = (input: $ReadOnlyArray<{{$Type.Singular}}>, options?: {{$Type.Singular}}UpdateMultipleOptions) => (dispatch: (ev: any) => void, getState: () => ({ {{$Type.LowerPlural}}: State })) => {
  const {{$Type.LowerPlural}} = getState().{{$Type.LowerPlural}}.{{$Type.LowerPlural}};

  const previous = input.map(({ id }) => {{$Type.LowerPlural}}.find(e => e.id === id));
  if (!previous.length) {
    return;
  }

  const timeoutHandle = getState().{{$Type.LowerPlural}}.timeouts[input.map(e => e.id).sort().join(',')];
  if (timeoutHandle) {
    clearTimeout(timeoutHandle);
    dispatch({ type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_CANCEL"}}', payload: { ids: input.map(e => e.id).sort().join(',') } });
  }

  dispatch({
    type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_BEGIN"}}',
    payload: {
      records: input,
      timeout: setTimeout(
        () =>
          void axios.put('/api/{{$Type.LowerPlural}}/_multi', { records: input }).then(
            ({ data: { records } }: { data: { records: $ReadOnlyArray<{{$Type.Singular}}> } }) => {
              dispatch({
                type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_COMPLETE"}}',
                payload: { records, options: options || {} },
              });

              if (options && options.after) {
                setImmediate(options.after, null, records);
              }
            },
            (err: Error) => {
              dispatch({
                type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_FAILED"}}',
                payload: { record: previous, error: errorsEnsureError(err) },
              });

              if (options && options.after) {
                setImmediate(options.after, err);
              }
            }
          ),
        options && typeof options.timeout === 'number'
          ? options.timeout
          : 1000
      ),
    },
  });
};
{{end}}

/** {{$Type.LowerPlural}}Reset resets the whole {{$Type.Singular}} state */
export const {{$Type.LowerPlural}}Reset = () => ({
  type: 'X/{{Hash $Type.LowerPlural "/RESET"}}',
  payload: {},
});

/** {{$Type.LowerPlural}}InvalidateCache invalidates the caches for {{$Type.Singular}} */
export const {{$Type.LowerPlural}}InvalidateCache = () => ({
  type: 'X/{{Hash $Type.LowerPlural "/INVALIDATE_CACHE"}}',
  payload: {},
});

const defaultState: State = {
  loading: 0,
  {{$Type.LowerPlural}}: [],
  searchCache: {},
  fetchCache: {},
  error: null,
  timeouts: {},
};

export default function reducer(state: State = defaultState, action: Action): State {
  switch (action.type) {
    case 'X/{{Hash $Type.LowerPlural "/SEARCH_BEGIN"}}': {
      const { params, key, page } = action.payload;

      return {
        ...state,
        loading: state.loading + 1,
        searchCache: updateSearchCacheLoading(state.searchCache, params, key, page, 1),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/SEARCH_COMPLETE"}}': {
      const { params, key, time, total, page, records } = action.payload;

      const ids = records.map((e) => e.id);

      return {
        ...state,
        loading: state.loading - 1,
        error: null,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        searchCache: updateSearchCacheComplete(state.searchCache, params, key, page, time, total, ids),
        fetchCache: updateFetchCachePushMulti(state.fetchCache, ids, time),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/SEARCH_FAILED"}}': {
      const { params, key, page, time, error } = action.payload;

      return {
        ...state,
        loading: state.loading - 1,
        error: error,
        searchCache: updateSearchCacheError(state.searchCache, params, key, page, time, error),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/FETCH_BEGIN"}}': {
      const { id } = action.payload;

      return {
        ...state,
        loading: state.loading + 1,
        fetchCache: updateFetchCacheLoading(state.fetchCache, id, 1),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/FETCH_COMPLETE_MULTI"}}': {
      const { ids, time, records } = action.payload;

      return {
        ...state,
        loading: state.loading - ids.length,
        error: null,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        fetchCache: updateFetchCacheCompleteMulti(state.fetchCache, ids, time),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/FETCH_FAILED_MULTI"}}': {
      const { ids, time, error } = action.payload;

      return {
        ...state,
        loading: state.loading - ids.length,
        error: error,
        fetchCache: updateFetchCacheErrorMulti(state.fetchCache, ids, time, error),
      };
    }
{{if $Type.CanCreate}}
    case 'X/{{Hash $Type.LowerPlural "/CREATE_BEGIN"}}':
      return {
        ...state,
        loading: state.loading + 1,
      };
    case 'X/{{Hash $Type.LowerPlural "/CREATE_COMPLETE"}}': {
      const { record, options } = action.payload;

      return {
        ...state,
        loading: options.push ? state.loading : state.loading - 1,
        error: null,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, [ record ]),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/CREATE_FAILED"}}':
      return {
        ...state,
        loading: state.loading - 1,
        error: action.payload.error,
      };
    case 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_BEGIN"}}': {
      const { records, options } = action.payload;

      return {
        ...state,
        loading: state.loading + 1,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_COMPLETE"}}': {
      const { records, options } = action.payload;

      return {
        ...state,
        loading: state.loading - 1,
        error: null,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_FAILED"}}': {
      const { records, options, error } = action.payload;
      const ids = records.map((e) => e.id);

      return {
        ...state,
        loading: state.loading - 1,
        error: error,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Type.LowerPlural}}: state.{{$Type.LowerPlural}}.filter((e) => ids.indexOf(e.id) === -1),
      };
    }
{{end}}
{{if $Type.CanUpdate}}
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_BEGIN"}}':
      return {
        ...state,
        loading: state.loading + 1,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, [
          action.payload.record,
        ]),
        timeouts: {
          ...state.timeouts,
          [action.payload.record.id]: action.payload.timeout,
        },
      };
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_CANCEL"}}':
      return {
        ...state,
        loading: state.loading - 1,
        timeouts: {
          ...state.timeouts,
          [action.payload.id]: null,
        },
      };
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_COMPLETE"}}': {
      const { record, options } = action.payload;

      return {
        ...state,
        loading: options.push ? state.loading : state.loading - 1,
        error: null,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, [ record ]),
        timeouts: {
          ...state.timeouts,
          [action.payload.record.id]: null,
        },
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_FAILED"}}':
      return {
        ...state,
        loading: state.loading - 1,
        error: action.payload.error,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, [
          action.payload.record,
        ]),
        timeouts: {
          ...state.timeouts,
          [action.payload.record.id]: null,
        },
      };
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_BEGIN"}}': {
      const { records, timeout } = action.payload;

      return {
        ...state,
        loading: state.loading + 1,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        timeouts: {
          ...state.timeouts,
          [records.map(e => e.id).sort().join(',')]: timeout,
        },
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_CANCEL"}}': {
      const { ids } = action.payload;

      return {
        ...state,
        loading: state.loading - 1,
        timeouts: {
          ...state.timeouts,
          [ids]: null,
        },
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_COMPLETE"}}': {
      const { records, options } = action.payload;

      return {
        ...state,
        loading: options.push ? state.loading : state.loading - 1,
        error: null,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        timeouts: {
          ...state.timeouts,
          [records.map(e => e.id).sort().join(',')]: null,
        },
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_FAILED"}}': {
      const { records, error } = action.payload;

      return {
        ...state,
        loading: state.loading - 1,
        error: error,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        timeouts: {
          ...state.timeouts,
          [records.map(e => e.id).sort().join(',')]: null,
        },
      };
    }
{{end}}
    case 'X/{{Hash $Type.LowerPlural "/RECORD_PUSH"}}': {
      const { time, record } = action.payload;

      return {
        ...state,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, [record]),
        fetchCache: updateFetchCachePushMulti(state.fetchCache, [record.id], time),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/RECORD_PUSH_MULTI"}}': {
      const { time, records } = action.payload;

      return {
        ...state,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        fetchCache: updateFetchCachePushMulti(state.fetchCache, records.map(e => e.id), time),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/INVALIDATE_CACHE"}}':
      return { ...state, searchCache: {}, fetchCache: {} };
    case 'X/{{Hash $Type.LowerPlural "/RESET"}}':
      return defaultState;
    case 'X/INVALIDATE': {
      const ids = action.payload["{{$Type.Singular}}"];

      if (!ids) {
        return state;
      }

      return {
        ...state,
        fetchCache: invalidateFetchCacheWithIDs(state.fetchCache, ids),
        searchCache: invalidateSearchCacheWithIDs(state.searchCache, ids),
      };
    }
    default:
      return state;
  }
}
`))
