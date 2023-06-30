package main

import (
	"strings"
)

type EnumGenerator struct {
	dir string
}

func NewEnumGenerator(dir string) *EnumGenerator {
	return &EnumGenerator{dir: dir}
}

func (g *EnumGenerator) Name() string {
	return "enum"
}

func (g *EnumGenerator) Model(model *Model) []writer {
	return []writer{
		&basicWriterForGo{
			basicWriter: basicWriter{
				name:     "individual",
				language: "go",
				file:     g.dir + "/modelenum/" + strings.ToLower(model.Singular) + "enum/" + strings.ToLower(model.Singular) + "enum.go",
				write:    templateWriter(enumTemplate, map[string]interface{}{"Model": model}),
			},
			packageName: strings.ToLower(model.Singular) + "enum",
		},
	}
}

var enumTemplate = `
{{$Model := .Model}}

{{- range $Field := $Model.Fields}}
{{- if $Field.Enum}}
const (
{{- range $Enum := $Field.Enum}}
	{{$Field.GoName}}{{$Enum.GoName}} = "{{$Enum.Value}}"
{{- end}}
)

var (
	Valid{{$Field.GoName}} = map[string]bool{
{{- range $Enum := $Field.Enum}}
		{{$Field.GoName}}{{$Enum.GoName}}: true,
{{- end}}
	}
	Values{{$Field.GoName}} = []string{
{{- range $Enum := $Field.Enum}}
		{{$Field.GoName}}{{$Enum.GoName}},
{{- end}}
	}
	Labels{{$Field.GoName}} = map[string]string{
{{- range $Enum := $Field.Enum}}
		{{$Field.GoName}}{{$Enum.GoName}}: "{{$Enum.Label}}",
{{- end}}
	}
)
{{- end}}
{{- end}}
`
