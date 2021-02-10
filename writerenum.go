package main

import (
	"go/types"
	"io"
	"strings"
	"text/template"
)

type EnumWriter struct{ dir string }

func NewEnumWriter(dir string) *EnumWriter { return &EnumWriter{dir: dir} }

func (EnumWriter) Name() string     { return "enum" }
func (EnumWriter) Language() string { return "go" }
func (w EnumWriter) File(typeName string, _ *types.Named, _ *types.Struct) string {
	return w.dir + "/modelenum/" + strings.ToLower(typeName) + "enum/" + strings.ToLower(typeName) + "enum.go"
}

func (w EnumWriter) PackageName(typeName string, _ *types.Named, _ *types.Struct) string {
	return strings.ToLower(typeName) + "enum"
}

func (EnumWriter) Imports(typeName string, namedType *types.Named, structType *types.Struct) []string {
	return []string{}
}

func (w *EnumWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	model, err := makeModel(typeName, namedType, structType)
	if err != nil {
		return err
	}
	return enumTemplate.Execute(wr, *model)
}

var enumTemplate = template.Must(template.New("enumTemplate").Funcs(tplFunc).Parse(`
{{$Type := .}}

{{- range $Field := $Type.Fields}}
{{- if $Field.Enum}}
const (
{{- range $Enum := $Field.Enum}}
  {{$Field.GoName}}{{$Enum.GoName}} = "{{$Enum.Value}}"
{{- end}}
)

var (
  Valid{{$Field.GoName}} = map[string]bool{}
  Values{{$Field.GoName}} = []string{}
  Labels{{$Field.GoName}} = map[string]string{
{{- range $Enum := $Field.Enum}}
    {{$Field.GoName}}{{$Enum.GoName}}: "{{$Enum.Label}}",
{{- end}}
  }
)

func init() {
  for k := range Labels{{$Field.GoName}} {
    Valid{{$Field.GoName}}[k] = true
    Values{{$Field.GoName}} = append(Values{{$Field.GoName}}, k)
  }
}
{{- end}}
{{- end}}
`))
