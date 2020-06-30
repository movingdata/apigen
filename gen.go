package main

import (
	"go/types"
	"io"
	"reflect"
	"regexp"
	"strings"
	"text/template"

	"github.com/danverbraganza/varcaser/varcaser"
	"github.com/grsmv/inflect"
)

type writer interface {
	Name() string
	Language() string
	File(typeName string) string
	Imports() []string
	Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error
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
