package main

import (
	"go/types"
	"io"
	"strings"
	"text/template"
)

type AccessorsWriter struct{ Dir string }

func NewAccessorsWriter(dir string) *AccessorsWriter { return &AccessorsWriter{Dir: dir} }

func (AccessorsWriter) Name() string     { return "accessors" }
func (AccessorsWriter) Language() string { return "go" }
func (w AccessorsWriter) File(typeName string, _ *types.Named, _ *types.Struct) string {
	return w.Dir + "/" + strings.ToLower(typeName) + "_accessors.go"
}

func (AccessorsWriter) Imports() []string {
	return []string{
		"fknsrs.biz/p/civil",
		"github.com/satori/go.uuid",
		"github.com/shopspring/decimal",
	}
}

func (w *AccessorsWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	model, err := makeModel(typeName, namedType, structType)
	if err != nil {
		return err
	}
	return accessorsTemplate.Execute(wr, *model)
}

var accessorsTemplate = template.Must(template.New("accessorsTemplate").Funcs(tplFunc).Parse(`
{{$Type := .}}

{{- range $Field := $Type.Fields}}
func ({{$Type.Singular | FirstChar}} *{{$Type.Singular}}) Set{{$Field.GoName}}(v {{$Field.GoType}}) {
  {{$Type.Singular | FirstChar}}.{{$Field.GoName}} = v
}
func ({{$Type.Singular | FirstChar}} *{{$Type.Singular}}) Get{{$Field.GoName}}() {{$Field.GoType}} {
  return {{$Type.Singular | FirstChar}}.{{$Field.GoName}}
}
func ({{$Type.Singular | FirstChar}} *{{$Type.Singular}}) Ptr{{$Field.GoName}}() *{{$Field.GoType}} {
  return &{{$Type.Singular | FirstChar}}.{{$Field.GoName}}
}
{{- end}}
`))
