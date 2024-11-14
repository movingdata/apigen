package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"go/types"
	"io"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"text/template"
	"unicode"

	"github.com/danverbraganza/varcaser/varcaser"
	"github.com/grsmv/inflect"
)

type generator interface {
	Name() string
}

type generatorForModel interface {
	generator
	Model(model *Model) []writer
}

type generatorForModels interface {
	generator
	Models(models []*Model) []writer
}

type writer interface {
	Name() string
	Language() string
	File() string
	Write(wr io.Writer) error
}

type writerForGo interface {
	PackageName() string
	Imports() []string
}

type basicWriter struct {
	name     string
	language string
	file     string
	write    func(wr io.Writer) error
}

func (w *basicWriter) Name() string             { return w.name }
func (w *basicWriter) Language() string         { return w.language }
func (w *basicWriter) File() string             { return w.file }
func (w *basicWriter) Write(wr io.Writer) error { return w.write(wr) }

type basicWriterForGo struct {
	basicWriter
	packageName string
	imports     []string
}

func (w *basicWriterForGo) PackageName() string { return w.packageName }
func (w *basicWriterForGo) Imports() []string   { return w.imports }

func templateWriter(tpl string, vars map[string]interface{}) func(wr io.Writer) error {
	t := template.Must(template.New("").Funcs(tplFunc).Parse(tpl))

	return func(wr io.Writer) error {
		return t.Execute(wr, vars)
	}
}

var headerTemplate = template.Must(template.New("header").Parse(`package {{.PackageName}}

import (
{{range $s := .Imports}}
	"{{$s}}"
{{- end}}
)

`))

var upperCaseOverrides = []string{
	"ADBOR",
	"API",
	"AVC",
	"BSI",
	"CIDN",
	"CMP",
	"CPE",
	"CVC",
	"EOMSYS",
	"FBS",
	"FNN",
	"FTTP",
	"ID",
	"IDEAL",
	"IP",
	"LOLO",
	"MAC",
	"MLO",
	"MNP",
	"NBN",
	"NTD",
	"OMFUL",
	"PM",
	"PMV",
	"PONR",
	"SIM",
	"SIP",
	"SMDM",
	"SQv2",
	"SQv3",
	"SQ",
	"TWI",
	"UI",
	"UNID",
	"UNIV",
	"UNMS",
	"VISP",
	"WAS",
	"WBI",
	"WLI",
	"WME",
}

var lowerCaseOverrides = []string{
	"a",
	"an",
	"and",
	"as",
	"at",
	"but",
	"by",
	"for",
	"if",
	"in",
	"nor",
	"of",
	"off",
	"on",
	"or",
	"per",
	"so",
	"the",
	"to",
	"up",
	"via",
	"yet",
}

func decorateWordCaseWithCaseOverrides(fn varcaser.WordCase, a ...[]string) varcaser.WordCase {
	return varcaser.WordCase(func(word string) string {
		for _, l := range a {
			for _, e := range l {
				if strings.ToLower(word) == strings.ToLower(e) {
					return e
				}
			}
		}

		return fn(word)
	})
}

func decorateCaseConventionWithCaseOverrides(c varcaser.CaseConvention) varcaser.CaseConvention {
	return varcaser.CaseConvention{
		JoinStyle:      c.JoinStyle,
		InitialCase:    decorateWordCaseWithCaseOverrides(c.InitialCase, upperCaseOverrides),
		SubsequentCase: decorateWordCaseWithCaseOverrides(c.SubsequentCase, upperCaseOverrides),
		Example:        c.Example,
	}
}

func inSlice(a []string, s string) bool {
	for _, e := range a {
		if e == s {
			return true
		}
	}

	return false
}

func splitWords(s string) []string {
	// NOTE(danver): While I keep finding new edge cases, I'll want
	// this to be easy-to-modify code rather than a regex.

	var words []string

	wasPreviousUpper := true
	current := []rune{}

	for _, c := range s {
		if wasPreviousUpper && unicode.IsUpper(c) {
			// If previous was uppercase, and this is
			// uppercase, continue the word.

			current = append(current, c)

			if inSlice(upperCaseOverrides, string(current)) {
				words = append(words, string(current))
				current = []rune{}
			}
		} else if wasPreviousUpper && !unicode.IsUpper(c) {
			// If the previous run was uppercase, but this
			// is not, set previous, but add it.

			// Edge case: the previous word was all uppercase.
			if len(current) > 1 {
				words = append(words, string(current[:len(current)-1]))
				current = current[len(current)-1:]
			}

			current = append(current, c)
			wasPreviousUpper = false
		} else if !wasPreviousUpper && unicode.IsUpper(c) {
			// If the previous rune was not uppercase, and
			// this character is, put current into
			// components first, then set wasPreviousUpper

			words = append(words, string(current))
			current = []rune{c}
			wasPreviousUpper = true
		} else if !wasPreviousUpper && !unicode.IsUpper(c) {
			// If the previous rune was not uppercase, and
			// this one is not, just add to this component.

			current = append(current, c)
		}
	}

	if len(current) != 0 {
		words = append(words, string(current))
	}

	return words
}

var (
	lowerCamelUpperCamelCaps *varcaser.Caser
	lowerKebabTitleCase      *varcaser.Caser
	lowerKebabUpperCamelCaps *varcaser.Caser
	upperCamelLowerCamel     *varcaser.Caser
	upperCamelLowerCamelCaps *varcaser.Caser
	upperCamelLowerSnake     *varcaser.Caser
)

func init() {
	lowerCamelUpperCamelCaps = &varcaser.Caser{
		From: varcaser.LowerCamelCase,
		To:   decorateCaseConventionWithCaseOverrides(varcaser.UpperCamelCaseKeepCaps),
	}
	lowerKebabTitleCase = &varcaser.Caser{
		From: varcaser.KebabCase,
		To: decorateCaseConventionWithCaseOverrides(varcaser.CaseConvention{
			JoinStyle:      varcaser.SimpleJoinStyle(" "),
			InitialCase:    decorateWordCaseWithCaseOverrides(strings.Title, upperCaseOverrides),
			SubsequentCase: decorateWordCaseWithCaseOverrides(strings.Title, upperCaseOverrides, lowerCaseOverrides),
			Example:        "Upper Title Case",
		}),
	}
	lowerKebabUpperCamelCaps = &varcaser.Caser{
		From: varcaser.KebabCase,
		To:   decorateCaseConventionWithCaseOverrides(varcaser.UpperCamelCase),
	}
	upperCamelLowerCamel = &varcaser.Caser{
		From: decorateCaseConventionWithCaseOverrides(varcaser.UpperCamelCase),
		To:   varcaser.LowerCamelCase,
	}
	upperCamelLowerCamelCaps = &varcaser.Caser{
		From: decorateCaseConventionWithCaseOverrides(varcaser.UpperCamelCase),
		To:   varcaser.LowerCamelCaseKeepCaps,
	}
	upperCamelLowerSnake = &varcaser.Caser{
		From: decorateCaseConventionWithCaseOverrides(varcaser.UpperCamelCase),
		To:   varcaser.LowerSnakeCase,
	}
}

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

func pluralFor(s string) string {
	a := splitWords(s)

	a[len(a)-1] = inflect.Pluralize(a[len(a)-1])

	for i, e := range a {
		a[i] = strings.ToLower(e)
	}

	return varcaser.LowerSnakeCase.Join(a)
}

var scanTypes = map[string]string{
	"[]time.Time":     "sqltypes.TimeArray",
	"[]time.Duration": "sqltypes.DurationArray",
	"[]*int":          "sqltypes.IntPointerArray",
}

var formatTypes = map[string]string{
	"string":    "%s",
	"uuid.UUID": "%s",
	"int":       "%d",
}

var jsTypes = map[string]string{
	"string":          "string",
	"int":             "number",
	"float64":         "number",
	"bool":            "boolean",
	"uuid.UUID":       "string",
	"time.Time":       "string",
	"time.Duration":   "string",
	"civil.Date":      "string",
	"json.RawMessage": "any",
}

type SwaggerType struct {
	Type     string        `json:"type"`
	Format   string        `json:"format,omitempty"`
	Nullable bool          `json:"nullable,omitempty"`
	Enum     []interface{} `json:"enum,omitempty"`
	Items    *SwaggerType  `json:"items,omitempty"`
}

func (t *SwaggerType) toMap() map[string]interface{} {
	if t.Type == "any" {
		return map[string]interface{}{}
	}

	m := map[string]interface{}{"type": t.Type}

	if t.Format != "" {
		m["format"] = t.Format
	}

	if t.Nullable {
		m["nullable"] = t.Nullable
	}

	if t.Enum != nil {
		m["enum"] = t.Enum
	}

	if t.Items != nil {
		m["items"] = t.Items.toMap()
	}

	return m
}

func getSwaggerType(goType string) *SwaggerType {
	switch goType {
	case "string":
		return &SwaggerType{Type: "string"}
	case "int":
		return &SwaggerType{Type: "number"}
	case "float64":
		return &SwaggerType{Type: "number"}
	case "bool":
		return &SwaggerType{Type: "boolean"}
	case "uuid.UUID":
		return &SwaggerType{Type: "string", Format: "uuid"}
	case "time.Time":
		return &SwaggerType{Type: "string", Format: "date-time"}
	case "time.Duration":
		return &SwaggerType{Type: "string"}
	case "civil.Date":
		return &SwaggerType{Type: "string", Format: "date"}
	case "json.RawMessage":
		return &SwaggerType{Type: "any"}
	default:
		return nil
	}
}

var flowTypes = map[string]string{
	"string":          "string",
	"int":             "number",
	"float64":         "number",
	"bool":            "boolean",
	"uuid.UUID":       "global_uuid_UUID",
	"time.Time":       "global_time_Time",
	"time.Duration":   "global_time_Duration",
	"civil.Date":      "global_civil_Date",
	"json.RawMessage": "any",
}

var sqlTypes = map[string]string{
	"string":          "text",
	"int":             "integer",
	"float64":         "double precision",
	"bool":            "boolean",
	"uuid.UUID":       "uuid",
	"time.Time":       "timestamp with time zone",
	"time.Duration":   "integer",
	"civil.Date":      "date",
	"json.RawMessage": "json",
}

var ignoreCreate = map[string]bool{
	"id":        true,
	"version":   true,
	"createdAt": true,
	"updatedAt": true,
	"creatorId": true,
	"updaterId": true,
}

var ignoreUpdate = map[string]bool{
	"id":        true,
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
	"LKUCC": func(s string) string { return lowerKebabUpperCamelCaps.String(s) },
	"UCLS":  func(s string) string { return upperCamelLowerSnake.String(s) },
	"Dump":  formatGo,
	"Default": func(defaultValue, input string) string {
		if input == "" {
			return defaultValue
		}

		return input
	},
	"PackageName": func(suffix, modelName string) string {
		return strings.ToLower(modelName) + suffix
	},
	"LC": func(s string) string {
		return strings.ToLower(s)
	},
	"UnPtr": func(s string) string {
		return strings.TrimPrefix(s, "*")
	},
	"Join": func(s1, s2 string) string {
		return s1 + s2
	},
	"EqualStrings": func(s1, s2 string) bool {
		return s1 == s2
	},
	"FormatTemplate": func(t string) string {
		switch t {
		case "uuid.UUID":
			return "%q"
		case "int":
			return "%d"
		default:
			return "%#v"
		}
	},
	"Equal": func(arg1, type1, arg2, type2 string) string {
		switch {
		case type1 == "bool" && type2 == "bool":
			return fmt.Sprintf("%s == %s", arg1, arg2)
		case type1 == "string" && type2 == "string":
			return fmt.Sprintf("%s == %s", arg1, arg2)
		case type1 == "*string" && type2 == "*string":
			return fmt.Sprintf("((%s == nil && %s == nil) || (%s != nil && %s != nil && *%s == *%s))", arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "[]string" && type2 == "[]string":
			return fmt.Sprintf("modelutil.EqualStringSlice(%s, %s)", arg1, arg2)
		case type1 == "int" && type2 == "int":
			return fmt.Sprintf("%s == %s", arg1, arg2)
		case type1 == "*int" && type2 == "*int":
			return fmt.Sprintf("((%s == nil && %s == nil) || (%s != nil && %s != nil && *%s == *%s))", arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "float64" && type2 == "float64":
			return fmt.Sprintf("%s == %s", arg1, arg2)
		case type1 == "*float64" && type2 == "*float64":
			return fmt.Sprintf("((%s == nil && %s == nil) || (%s != nil && %s != nil && *%s == *%s))", arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "uuid.UUID" && type2 == "uuid.UUID":
			return fmt.Sprintf("%s == %s", arg1, arg2)
		case type1 == "*uuid.UUID" && type2 == "*uuid.UUID":
			return fmt.Sprintf("((%s == nil && %s == nil) || (%s != nil && %s != nil && *%s == *%s))", arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "[]uuid.UUID" && type2 == "[]uuid.UUID":
			return fmt.Sprintf("modelutil.EqualUUIDSlice(%s, %s)", arg1, arg2)
		case type1 == "time.Time" && type2 == "time.Time":
			return fmt.Sprintf("%s.Equal(%s)", arg1, arg2)
		case type1 == "*time.Time" && type2 == "*time.Time":
			return fmt.Sprintf("((%s == nil && %s == nil) || (%s != nil && %s != nil && %s.Equal(*%s)))", arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "[]time.Time" && type2 == "[]time.Time":
			return fmt.Sprintf("modelutil.EqualTimeSlice(%s, %s)", arg1, arg2)
		case type1 == "sqltypes.TimeArray" && type2 == "sqltypes.TimeArray":
			return fmt.Sprintf("modelutil.EqualTimeArray(%s, %s)", arg1, arg2)
		case type1 == "time.Duration" && type2 == "time.Duration":
			return fmt.Sprintf("%s == %s", arg1, arg2)
		case type1 == "*time.Duration" && type2 == "*time.Duration":
			return fmt.Sprintf("((%s == nil && %s == nil) || (%s != nil && %s != nil && *%s == *%s))", arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "[]time.Duration" && type2 == "[]time.Duration":
			return fmt.Sprintf("modelutil.EqualDurationSlice(%s, %s)", arg1, arg2)
		case type1 == "sqltypes.DurationArray" && type2 == "sqltypes.DurationArray":
			return fmt.Sprintf("modelutil.EqualDurationArray(%s, %s)", arg1, arg2)
		case type1 == "civil.Date" && type2 == "civil.Date":
			return fmt.Sprintf("%s.On(%s)", arg1, arg2)
		case type1 == "*civil.Date" && type2 == "*civil.Date":
			return fmt.Sprintf("((%s == nil && %s == nil) || (%s != nil && %s != nil && %s.On(*%s)))", arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "json.RawMessage" && type2 == "json.RawMessage":
			return fmt.Sprintf("modelutil.EqualJSON(%s, %s)", arg1, arg2)
		default:
			fmt.Printf("UNKNOWN TYPES %q vs %q\n", type1, type2)
			return fmt.Sprintf("%q == %q", arg1+"<UNKNOWN_TYPE "+type1+">", arg2+"<UNKNOWN_TYPE "+type2+">")
		}
	},
	"NotEqual": func(arg1, type1, arg2, type2 string) string {
		switch {
		case type1 == "bool" && type2 == "bool":
			return fmt.Sprintf("%s != %s", arg1, arg2)
		case type1 == "string" && type2 == "string":
			return fmt.Sprintf("%s != %s", arg1, arg2)
		case type1 == "*string" && type2 == "*string":
			return fmt.Sprintf("((%s == nil && %s != nil) || (%s != nil && %s == nil) || (%s != nil && %s != nil && *%s != *%s))", arg1, arg2, arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "[]string" && type2 == "[]string":
			return fmt.Sprintf("!modelutil.EqualStringSlice(%s, %s)", arg1, arg2)
		case type1 == "int" && type2 == "int":
			return fmt.Sprintf("%s != %s", arg1, arg2)
		case type1 == "*int" && type2 == "*int":
			return fmt.Sprintf("((%s == nil && %s != nil) || (%s != nil && %s == nil) || (%s != nil && %s != nil && *%s != *%s))", arg1, arg2, arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "[]*int" && type2 == "[]*int":
			return fmt.Sprintf("!modelutil.Equal(%s, %s)", arg1, arg2)
		case type1 == "float64" && type2 == "float64":
			return fmt.Sprintf("%s != %s", arg1, arg2)
		case type1 == "*float64" && type2 == "*float64":
			return fmt.Sprintf("((%s == nil && %s != nil) || (%s != nil && %s == nil) || (%s != nil && %s != nil && *%s != *%s))", arg1, arg2, arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "uuid.UUID" && type2 == "uuid.UUID":
			return fmt.Sprintf("%s != %s", arg1, arg2)
		case type1 == "*uuid.UUID" && type2 == "*uuid.UUID":
			return fmt.Sprintf("((%s == nil && %s != nil) || (%s != nil && %s == nil) || (%s != nil && %s != nil && *%s != *%s))", arg1, arg2, arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "[]uuid.UUID" && type2 == "[]uuid.UUID":
			return fmt.Sprintf("!modelutil.EqualUUIDSlice(%s, %s)", arg1, arg2)
		case type1 == "time.Time" && type2 == "time.Time":
			return fmt.Sprintf("!%s.Equal(%s)", arg1, arg2)
		case type1 == "*time.Time" && type2 == "*time.Time":
			return fmt.Sprintf("((%s == nil && %s != nil) || (%s != nil && %s == nil) || (%s != nil && %s != nil && !%s.Equal(*%s)))", arg1, arg2, arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "[]time.Time" && type2 == "[]time.Time":
			return fmt.Sprintf("!modelutil.EqualTimeSlice(%s, %s)", arg1, arg2)
		case type1 == "sqltypes.TimeArray" && type2 == "sqltypes.TimeArray":
			return fmt.Sprintf("!modelutil.EqualTimeArray(%s, %s)", arg1, arg2)
		case type1 == "time.Duration" && type2 == "time.Duration":
			return fmt.Sprintf("!%s.Equal(%s)", arg1, arg2)
		case type1 == "*time.Duration" && type2 == "*time.Duration":
			return fmt.Sprintf("((%s == nil && %s != nil) || (%s != nil && %s == nil) || (%s != nil && %s != nil && *%s != *%s))", arg1, arg2, arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "[]time.Duration" && type2 == "[]time.Duration":
			return fmt.Sprintf("!modelutil.EqualDurationSlice(%s, %s)", arg1, arg2)
		case type1 == "sqltypes.DurationArray" && type2 == "sqltypes.DurationArray":
			return fmt.Sprintf("!modelutil.EqualDurationArray(%s, %s)", arg1, arg2)
		case type1 == "civil.Date" && type2 == "civil.Date":
			return fmt.Sprintf("!%s.On(%s)", arg1, arg2)
		case type1 == "*civil.Date" && type2 == "*civil.Date":
			return fmt.Sprintf("((%s == nil && %s != nil) || (%s != nil && %s == nil) || (%s != nil && %s != nil && !%s.On(*%s)))", arg1, arg2, arg1, arg2, arg1, arg2, arg1, arg2)
		case type1 == "json.RawMessage" && type2 == "json.RawMessage":
			return fmt.Sprintf("!modelutil.EqualJSON(%s, %s)", arg1, arg2)
		default:
			fmt.Printf("UNKNOWN TYPES %q vs %q\n", type1, type2)
			return fmt.Sprintf("%q == %q", arg1+"<UNKNOWN_TYPE "+type1+">", arg2+"<UNKNOWN_TYPE "+type2+">")
		}
	},
}

type Model struct {
	Singular         string
	Plural           string
	LowerPlural      string
	LowerSnakePlural string

	IDField      *Field
	VersionField *Field
	Fields       FieldList

	SpecialOrders  []SpecialOrder
	SpecialFilters []Filter

	Processes []string

	HasID        bool
	HasVersion   bool
	HasCreatedAt bool
	HasUpdatedAt bool
	HasCreatorID bool
	HasUpdaterID bool

	HasAudit      bool
	HasUserFilter bool

	HasAPISearch bool
	HasAPIGet    bool
	HasAPICreate bool
	HasAPIUpdate bool

	SQLTableName       string
	HasSQLFindOne      bool
	HasSQLFindOneByID  bool
	HasSQLFindMultiple bool
	HasSQLCreate       bool
	HasSQLSave         bool
}

type Field struct {
	IsNull bool
	Array  bool

	GoName     string
	GoType     string
	ScanType   string
	FormatType string

	SQLName string
	SQLType string

	APIName     string
	APIRefs     []APIRef
	JSType      string
	FlowType    string
	SwaggerType *SwaggerType

	Filters []Filter

	IgnoreCreate   bool
	IgnoreUpdate   bool
	OmitEmpty      bool
	Enum           EnumList
	Sequence       string
	SequencePrefix string
}

func (f Field) HasEnumValue(value string) bool {
	return f.Enum.HasValue(value)
}

func (f Field) HasEnumValues(values []string) bool {
	return f.Enum.HasValues(values)
}

type FieldList []Field

func (l FieldList) GetByName(name string) *Field {
	for _, f := range l {
		if f.GoName == name || f.SQLName == name || f.APIName == name {
			return &f
		}
	}

	return nil
}

func (l FieldList) GetByNameAndType(name, typ string) *Field {
	if f := l.GetByName(name); f != nil {
		if f.GoType == typ || f.SQLType == typ || f.JSType == typ || f.FlowType == typ {
			return f
		}
	}

	return nil
}

func (l FieldList) HasFieldWithNameAndType(name, typ string) bool {
	return l.GetByNameAndType(name, typ) != nil
}

func (l FieldList) HasFieldsWithNamesAndTypes(pairs [][2]string) bool {
	for _, pair := range pairs {
		if !l.HasFieldWithNameAndType(pair[0], pair[1]) {
			return false
		}
	}

	return true
}

type APIRef struct {
	ModelName string
	FieldName string
}

type Enum struct {
	Value  string
	Label  string
	GoName string
}

type EnumList []Enum

func (l EnumList) First() Enum {
	return l[0]
}

func (l EnumList) Values() []string {
	a := make([]string, len(l))
	for i, e := range l {
		a[i] = e.Value
	}

	return a
}

func (l EnumList) GetByValue(value string) *Enum {
	for _, e := range l {
		if e.Value == value {
			return &e
		}
	}

	return nil
}

func (l EnumList) HasValue(value string) bool {
	return l.GetByValue(value) != nil
}

func (l EnumList) HasValues(values []string) bool {
	for _, e := range values {
		if !l.HasValue(e) {
			return false
		}
	}

	return true
}

type SpecialOrder struct {
	GoName  string
	APIName string
}

type Filter struct {
	Operator    string
	Name        string
	GoName      string
	GoType      string
	JSType      string
	FlowType    string
	SwaggerType *SwaggerType
}

func makeModel(typeName string, namedType *types.Named, structType *types.Struct) (*Model, error) {
	words := splitWords(typeName)

	words[len(words)-1] = inflect.Pluralize(words[len(words)-1])

	for i, e := range words {
		if i == 0 {
			words[i] = upperCamelLowerCamelCaps.To.InitialCase(e)
		} else {
			words[i] = upperCamelLowerCamelCaps.To.SubsequentCase(e)
		}
	}

	var (
		lowerPlural        = upperCamelLowerCamelCaps.To.Join(words)
		lowerSnakePlural   = pluralFor(typeName)
		sqlTableName       = pluralFor(typeName)
		fields             FieldList
		specialOrders      []SpecialOrder
		specialFilters     []Filter
		hasID              = false
		hasVersion         = false
		hasCreatedAt       = false
		hasUpdatedAt       = false
		hasCreatorID       = false
		hasUpdaterID       = false
		hasUserFilter      = false
		hasAPINoAudit      = false
		hasAPINoSearch     = false
		hasAPINoGet        = false
		hasAPINoCreate     = false
		hasAPINoUpdate     = false
		hasSQLFindOne      = false
		hasSQLFindOneByID  = false
		hasSQLFindMultiple = false
		hasSQLCreate       = false
		hasSQLSave         = false
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
				apiName = upperCamelLowerCamel.String(f.Name())
			}
		}

		if a, ok := apiTagOptions["lowerPlural"]; ok && len(a) > 0 && len(a[0]) > 0 {
			lowerPlural = a[0][0]
		}

		if apiName == "id" {
			hasID = true
		}
		if apiName == "version" {
			hasVersion = true
		}
		if apiName == "createdAt" {
			hasCreatedAt = true
		}
		if apiName == "creatorId" {
			hasCreatorID = true
		}
		if apiName == "updatedAt" {
			hasUpdatedAt = true
		}
		if apiName == "updaterId" {
			hasUpdaterID = true
		}

		if _, ok := apiTagOptions["noaudit"]; ok {
			hasAPINoAudit = true
		}
		if _, ok := apiTagOptions["nosearch"]; ok {
			hasAPINoSearch = true
		}
		if _, ok := apiTagOptions["noget"]; ok {
			hasAPINoGet = true
		}
		if _, ok := apiTagOptions["nocreate"]; ok {
			hasAPINoCreate = true
		}
		if _, ok := apiTagOptions["noupdate"]; ok {
			hasAPINoUpdate = true
		}

		sqlName, sqlTagOptions := getAndParseTagIndex(structType, i, "sql")
		if sqlName == "" {
			sqlName = upperCamelLowerSnake.String(f.Name())
		}

		if a, ok := sqlTagOptions["table"]; ok && len(a) > 0 {
			sqlTableName = a[0][0]
		}

		if _, ok := sqlTagOptions["findOne"]; ok {
			hasSQLFindOne = true
		}
		if _, ok := sqlTagOptions["findOneByID"]; ok {
			hasSQLFindOneByID = true
		}
		if _, ok := sqlTagOptions["findMultiple"]; ok {
			hasSQLFindMultiple = true
		}
		if _, ok := sqlTagOptions["create"]; ok {
			hasSQLCreate = true
		}
		if _, ok := sqlTagOptions["save"]; ok {
			hasSQLSave = true
		}

		var enums EnumList
		if s := getTagIndex(structType, i, "enum"); s != "" {
			a := strings.Split(s[1:], string(s[0]))

			enums = make(EnumList, len(a))

			for i, s := range a {
				b := strings.SplitN(s, ":", 3)
				if len(b) == 1 {
					b = append(b, lowerKebabTitleCase.String(b[0]))
				}
				if len(b) == 2 {
					v := b[0]
					if v == "" {
						v = "empty"
					}

					b = append(b, lowerKebabUpperCamelCaps.String(v))
				}

				label, err := url.QueryUnescape(b[1])
				if err != nil {
					return nil, fmt.Errorf("bad percent-encoding in enum; field=%v.%v: %w", namedType.String(), f.Name(), err)
				}
				b[1] = label

				enums[i] = Enum{b[0], b[1], b[2]}
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
				return nil, fmt.Errorf("can't specify omitEmpty with enum unless one is an empty string; field=%v.%v", namedType.String(), f.Name())
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
				return nil, fmt.Errorf("sequence option needs exactly one or two parameters")
			}
		}

		gf := Field{
			GoName:         f.Name(),
			APIName:        apiName,
			SQLName:        sqlName,
			IgnoreCreate:   ignoreCreate[apiName],
			IgnoreUpdate:   ignoreUpdate[apiName],
			OmitEmpty:      omitEmpty,
			Enum:           enums,
			Sequence:       sequence,
			SequencePrefix: sequencePrefix,
		}

		var goType string

		switch ft := ft.(type) {
		case *types.Basic:
			goType = ft.String()
		case *types.Named:
			goType = ft.Obj().Pkg().Name() + "." + ft.Obj().Name()
		default:
			return nil, fmt.Errorf("unrecognised field type %s (%s)", ft.String(), f.Name())
		}

		jsType := jsTypes[goType]
		flowType := flowTypes[goType]
		swaggerType := getSwaggerType(goType)
		sqlType := sqlTypes[goType]

		if goType == "" {
			return nil, fmt.Errorf("couldn't determine go type for %s (%s)", ft, f.Name())
		}
		if jsType == "" {
			return nil, fmt.Errorf("couldn't determine js type for %s (%s)", ft, f.Name())
		}
		if flowType == "" {
			return nil, fmt.Errorf("couldn't determine js type for %s (%s)", ft, f.Name())
		}
		if sqlType == "" {
			return nil, fmt.Errorf("couldn't determine sql type for %s (%s)", ft, f.Name())
		}

		var jsEnums []string
		var flowEnums []string
		var swaggerEnums []interface{}

		if len(enums) > 0 {
			switch jsType {
			case "string":
				jsEnums = make([]string, len(enums))
				flowEnums = make([]string, len(enums))
				swaggerEnums = make([]interface{}, len(enums))
				for i := range enums {
					jsEnums[i] = "'" + enums[i].Value + "'"
					flowEnums[i] = "'" + enums[i].Value + "'"
					swaggerEnums[i] = enums[i].Value
				}
			case "number":
				jsEnums = make([]string, len(enums))
				flowEnums = make([]string, len(enums))
				swaggerEnums = make([]interface{}, len(enums))
				for i := range enums {
					jsEnums[i] = enums[i].Value
					flowEnums[i] = enums[i].Value
					swaggerEnums[i] = enums[i].Value
				}
			default:
				return nil, fmt.Errorf("got enum values for %q but can't make js type for %q", f.Name(), jsType)
			}
		}

		if len(jsEnums) > 0 {
			jsType = typeName + gf.GoName
			flowType = "global_db_" + typeName + gf.GoName
		}

		if len(swaggerEnums) > 0 {
			swaggerType.Enum = swaggerEnums
		}

		gf.GoType = goType
		gf.JSType = jsType
		gf.FlowType = flowType
		gf.SwaggerType = swaggerType
		gf.SQLType = sqlType

		if isPointer {
			gf.IsNull = true
			gf.GoType = "*" + gf.GoType
			gf.JSType = "?" + gf.JSType
			gf.FlowType = "?" + gf.FlowType
			gf.SwaggerType.Nullable = true
		}
		if isSlice {
			gf.Array = true
			gf.GoType = "[]" + gf.GoType
			gf.JSType = "$ReadOnlyArray<" + gf.JSType + ">"
			gf.FlowType = "$ReadOnlyArray<" + gf.FlowType + ">"
			gf.SwaggerType = &SwaggerType{Type: "array", Items: gf.SwaggerType}
		}

		if scanType := scanTypes[gf.GoType]; scanType != "" {
			gf.ScanType = scanType
		}

		if formatType := formatTypes[gf.GoType]; formatType != "" {
			gf.FormatType = formatType
		}

		for range apiTagOptions["userFilter"] {
			hasUserFilter = true
		}

		for _, opts := range apiTagOptions["specialOrder"] {
			o := SpecialOrder{APIName: apiName, GoName: f.Name()}

			if len(opts) > 0 && opts[0] != "" {
				o.APIName = opts[0]
			}
			if len(opts) > 1 && opts[1] != "" {
				o.GoName = opts[1]
			}

			specialOrders = append(specialOrders, o)
		}

		for _, opts := range apiTagOptions["specialFilter"] {
			goName := lowerCamelUpperCamelCaps.String(apiName)
			if len(opts) > 0 && opts[0] != "" {
				goName = opts[0]
			}

			name := apiName
			if len(opts) > 1 && opts[1] != "" {
				name = opts[1]
			}

			f := Filter{
				Operator:    "=",
				Name:        name,
				GoName:      goName,
				JSType:      jsType,
				FlowType:    flowType,
				SwaggerType: swaggerType,
			}

			switch ft := ft.(type) {
			case *types.Basic:
				f.GoType = "*" + ft.String()
			case *types.Named:
				f.GoType = "*" + ft.Obj().Pkg().Name() + "." + ft.Obj().Name()
			}

			switch f.Operator {
			case "is_null", "is_not_null":
				f.GoType = "*bool"
				f.JSType = "boolean"
				f.FlowType = "boolean"
				f.SwaggerType = &SwaggerType{Type: "string", Enum: []interface{}{"true", "false"}}
			case "in", "not_in", "@>", "!@>", "<@", "!<@", "&&", "!&&":
				f.GoType = "[]" + strings.TrimPrefix(f.GoType, "*")
				f.JSType = "$ReadOnlyArray<" + strings.TrimPrefix(f.JSType, "?") + ">"
				f.FlowType = "$ReadOnlyArray<" + strings.TrimPrefix(f.FlowType, "?") + ">"
				f.SwaggerType = &SwaggerType{Type: "array", Items: f.SwaggerType}
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
			case "int", "*int", "float64", "*float64":
				filterOptions = [][]string{{"="}, {"!="}, {"<"}, {"<="}, {">"}, {">="}}
			case "[]uuid.UUID", "[]string", "[]int":
				filterOptions = [][]string{{"@>"}, {"!@>"}, {"<@"}, {"!<@"}, {"&&"}, {"!&&"}}
			case "bool", "*bool":
				filterOptions = [][]string{{"="}, {"!="}}
			case "time.Time", "*time.Time", "time.Duration", "*time.Duration", "civil.Date", "*civil.Date":
				filterOptions = [][]string{{"="}, {"!="}, {"<"}, {"<="}, {">"}, {">="}}

				if isPointer {
					filterOptions = append(
						filterOptions,
						[]string{"is_null_or_less_than"},
						[]string{"is_null_or_less_than_or_equal_to"},
						[]string{"is_null_or_greater_than"},
						[]string{"is_null_or_greater_than_or_equal_to"},
					)
				}
			}

			if isPointer {
				filterOptions = append(filterOptions, []string{"is_null"}, []string{"is_not_null"})
			}

			if len(swaggerEnums) > 0 {
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
			filterJSType := jsType
			filterFlowType := flowType
			filterSwaggerType := swaggerType
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
			case "is_null_or_less_than":
				jsName = jsName + "IsNullOrLessThan"
				goName = goName + "IsNullOrLessThan"
			case "is_null_or_less_than_or_equal_to":
				jsName = jsName + "IsNullOrLessThanOrEqualTo"
				goName = goName + "IsNullOrLessThanOrEqualTo"
			case ">":
				jsName = jsName + "Gt"
				goName = goName + "Gt"
			case ">=":
				jsName = jsName + "Gte"
				goName = goName + "Gte"
			case "is_null_or_greater_than":
				jsName = jsName + "IsNullOrGreaterThan"
				goName = goName + "IsNullOrGreaterThan"
			case "is_null_or_greater_than_or_equal_to":
				jsName = jsName + "IsNullOrGreaterThanOrEqualTo"
				goName = goName + "IsNullOrGreaterThanOrEqualTo"
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
				filterFlowType = "boolean"
				filterSwaggerType = &SwaggerType{Type: "string", Enum: []interface{}{"true", "false"}}
			case "is_not_null":
				jsName = jsName + "IsNotNull"
				goName = goName + "IsNotNull"
				filterJSType = "boolean"
				filterFlowType = "boolean"
				filterSwaggerType = &SwaggerType{Type: "string", Enum: []interface{}{"true", "false"}}
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

			gff := Filter{
				Operator:    operator,
				Name:        jsName,
				GoName:      goName,
				JSType:      filterJSType,
				FlowType:    filterFlowType,
				SwaggerType: filterSwaggerType,
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
				gff.FlowType = "boolean"
			case "in", "not_in", "@>", "!@>", "<@", "!<@", "&&", "!&&":
				gff.GoType = "[]" + strings.TrimPrefix(gff.GoType, "*")
				gff.JSType = "$ReadOnlyArray<" + strings.TrimPrefix(gff.JSType, "?") + ">"
				gff.FlowType = "$ReadOnlyArray<" + strings.TrimPrefix(gff.FlowType, "?") + ">"
			}

			gf.Filters = append(gf.Filters, gff)
		}

		for _, opts := range apiTagOptions["ref"] {
			var modelName, fieldName string
			switch len(opts) {
			case 1:
				modelName = opts[0]
				fieldName = "ID"
			case 2:
				modelName = opts[0]
				fieldName = opts[1]
			default:
				return nil, fmt.Errorf("bad ref option (%v); field=%v.%v", opts, namedType.String(), f.Name())
			}

			gf.APIRefs = append(gf.APIRefs, APIRef{
				ModelName: modelName,
				FieldName: fieldName,
			})
		}

		if f.Exported() {
			fields = append(fields, gf)
		}
	}

	var processes []string

	for _, f := range fields {
		if !strings.HasSuffix(f.GoName, "JobID") {
			continue
		}

		processName := strings.TrimSuffix(f.GoName, "JobID")

		if !fields.HasFieldsWithNamesAndTypes([][2]string{
			{processName + "Status", "string"},
			{processName + "JobID", "*int"},
			{processName + "StartedAt", "*time.Time"},
			{processName + "Deadline", "*time.Time"},
			{processName + "FailureMessage", "string"},
			{processName + "CompletedAt", "*time.Time"},
		}) {
			continue
		}

		if !fields.GetByName(processName + "Status").HasEnumValues([]string{
			"in-progress",
			"completed",
			"failed",
		}) {
			continue
		}

		processes = append(processes, processName)
	}

	return &Model{
		Singular:           typeName,
		Plural:             inflect.Camelize(strings.Join(words, "_")),
		LowerPlural:        lowerPlural,
		LowerSnakePlural:   lowerSnakePlural,
		SQLTableName:       sqlTableName,
		Fields:             fields,
		IDField:            fields.GetByName("ID"),
		VersionField:       fields.GetByName("Version"),
		Processes:          processes,
		SpecialOrders:      specialOrders,
		SpecialFilters:     specialFilters,
		HasID:              hasID,
		HasVersion:         hasVersion,
		HasCreatedAt:       hasCreatedAt,
		HasUpdatedAt:       hasUpdatedAt,
		HasCreatorID:       hasCreatorID,
		HasUpdaterID:       hasUpdaterID,
		HasAudit:           hasAPINoAudit == false,
		HasUserFilter:      hasUserFilter,
		HasAPISearch:       hasAPINoSearch == false,
		HasAPIGet:          hasAPINoGet == false,
		HasAPICreate:       hasAPINoCreate == false && hasCreatedAt,
		HasAPIUpdate:       hasAPINoUpdate == false && hasUpdatedAt,
		HasSQLFindOne:      hasSQLFindOne,
		HasSQLFindOneByID:  hasSQLFindOneByID,
		HasSQLFindMultiple: hasSQLFindMultiple,
		HasSQLCreate:       hasSQLCreate,
		HasSQLSave:         hasSQLSave,
	}, nil
}
