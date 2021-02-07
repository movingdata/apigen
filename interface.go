package main

import (
	"go/types"
	"io"
	"strings"
	"text/template"
)

type InterfaceWriter struct{ Dir string }

func NewInterfaceWriter(dir string) *InterfaceWriter { return &InterfaceWriter{Dir: dir} }

func (InterfaceWriter) Name() string     { return "interface" }
func (InterfaceWriter) Language() string { return "go" }
func (w InterfaceWriter) File(typeName string, _ *types.Named, _ *types.Struct) string {
	return w.Dir + "/interfaces/" + strings.ToLower(typeName) + "interface/" + strings.ToLower(typeName) + ".go"
}

func (w InterfaceWriter) PackageName(typeName string, _ *types.Named, _ *types.Struct) string {
  return strings.ToLower(typeName) + "interface"
}

func (InterfaceWriter) Imports() []string {
	return []string{
		"fknsrs.biz/p/civil",
		"github.com/satori/go.uuid",
		"github.com/shopspring/decimal",
	}
}

func (w *InterfaceWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	model, err := makeModel(typeName, namedType, structType)
	if err != nil {
		return err
	}
	return interfaceTemplate.Execute(wr, *model)
}

var interfaceTemplate = template.Must(template.New("interfaceTemplate").Funcs(tplFunc).Parse(`
{{$Type := .}}

type {{$Type.Singular}} interface{
{{- range $Field := $Type.Fields}}
	Set{{$Field.GoName}}(v {{$Field.GoType}})
	Get{{$Field.GoName}}() {{$Field.GoType}}
	Ptr{{$Field.GoName}}() *{{$Field.GoType}}
{{- end}}
}
`))
