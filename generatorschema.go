package main

import (
	"strings"
)

type SchemaGenerator struct {
	dir string
}

func NewSchemaGenerator(dir string) *SchemaGenerator {
	return &SchemaGenerator{dir: dir}
}

func (g *SchemaGenerator) Name() string {
	return "schema"
}

func (g *SchemaGenerator) Model(model *Model) []writer {
	return []writer{
		&basicWriterForGo{
			basicWriter: basicWriter{
				name:     "individual",
				language: "go",
				file:     g.dir + "/modelschema/" + strings.ToLower(model.Singular) + "schema/" + strings.ToLower(model.Singular) + "schema.go",
				write:    templateWriter(schemaTemplate, map[string]interface{}{"Model": model}),
			},
			packageName: strings.ToLower(model.Singular) + "schema",
			imports: []string{
				"movingdata.com/p/wbi/internal/apitypes",
				"fknsrs.biz/p/sqlbuilder",
			},
		},
	}
}

func (g *SchemaGenerator) Models(models []*Model) []writer {
	return []writer{
		&basicWriterForGo{
			basicWriter: basicWriter{
				name:     "aggregated",
				language: "go",
				file:     g.dir + "/modelschema/models.go",
				write:    templateWriter(schemaFinishTemplate, map[string]interface{}{"Models": models}),
			},
			packageName: "modelschema",
			imports: []string{
				"movingdata.com/p/wbi/internal/apitypes",
				"fknsrs.biz/p/sqlbuilder",
			},
		},
	}
}

var schemaTemplate = `
{{$Model := .Model}}

// Table is a symbolic identifier for the "{{$Model.SQLTableName}}" table
var Table = sqlbuilder.NewTable(
	"{{$Model.SQLTableName}}",
{{- range $Field := $Model.Fields}}
	"{{$Field.SQLName}}",
{{- end}}
)

var (
{{- range $Field := $Model.Fields}}
	// Column{{$Field.GoName}} is a symbolic identifier for the "{{$Model.SQLTableName}}"."{{$Field.SQLName}}" column
	Column{{$Field.GoName}} = Table.C("{{$Field.SQLName}}")
{{- end}}
)

// Columns is a list of columns in the "{{$Model.SQLTableName}}" table
var Columns = []*sqlbuilder.BasicColumn{
{{- range $Field := $Model.Fields}}
	Column{{$Field.GoName}},
{{- end}}
}

var (
{{- range $Field := $Model.Fields}}
	// Field{{$Field.GoName}} is a symbolic identifier for the "{{$Model.Singular}}"."{{$Field.GoName}}" field schema
	Field{{$Field.GoName}} = &apitypes.Field{
		GoName: "{{$Field.GoName}}",
		GoType: "{{$Field.GoType}}",
		SQLName: "{{$Field.SQLName}}",
		SQLType: "{{$Field.SQLType}}",
		APIName: "{{$Field.APIName}}",
		APIType: "{{$Field.JSType}}",
		Array: {{if $Field.Array}}true{{else}}false{{end}},
		NotNull: {{if $Field.IsNull}}false{{else}}true{{end}},
{{- if $Field.Enum}}
		Enum: []apitypes.Enum{
{{- range $Enum := $Field.Enum}}
			apitypes.Enum{Value: "{{$Enum.Value}}", Label: "{{$Enum.Label}}"},
{{- end}}
		},
{{- end}}
		Filters: []*apitypes.Filter{
{{- range $Filter := $Field.Filters}}
			&apitypes.Filter{Operator: "{{$Filter.Operator}}", Name: "{{$Filter.Name}}", GoName: "{{$Filter.GoName}}", GoType: "{{$Filter.GoType}}"},
{{- end}}
		},
	}
{{- end}}
)

var Model = &apitypes.Model{
	GoName: "{{$Model.Singular}}",
	SQLName: "{{$Model.SQLTableName}}",
	APIName: "{{$Model.LowerPlural}}",
	Fields: []*apitypes.Field{
{{- range $Field := $Model.Fields}}
		Field{{$Field.GoName}},
{{- end}}
	},
	SpecialFilters: []*apitypes.Filter{
{{- range $Filter := $Model.SpecialFilters}}
		&apitypes.Filter{Operator: "{{$Filter.Operator}}", Name: "{{$Filter.Name}}", GoName: "{{$Filter.GoName}}", GoType: "{{$Filter.GoType}}"},
{{- end}}
	},
}

func init() {
	Model.FlattenFilters()
}

var Relations = []*apitypes.Relation{
{{- range $Field := $Model.Fields}}
{{- range $Ref := $Field.APIRefs}}
	&apitypes.Relation{
		SourceModel: "{{$Model.Singular}}",
		SourceField: "{{$Field.GoName}}",
		TargetModel: "{{$Ref.ModelName}}",
		TargetField: "{{$Ref.FieldName}}",
	},
{{- end}}
{{- end}}
}
`

var schemaFinishTemplate = `
{{$Models := .Models}}

var Tables = map[string]*sqlbuilder.Table{
{{- range $Model := $Models}}
	{{PackageName "schema" $Model.Singular}}.Model.GoName: {{PackageName "schema" $Model.Singular}}.Table,
	{{PackageName "schema" $Model.Singular}}.Model.SQLName: {{PackageName "schema" $Model.Singular}}.Table,
{{- end}}
}

var Models = map[string]*apitypes.Model{
{{- range $Model := $Models}}
	{{PackageName "schema" $Model.Singular}}.Model.GoName: {{PackageName "schema" $Model.Singular}}.Model,
{{- end}}
}

var Relations = flattenRelations(
{{- range $Model := $Models}}
	{{PackageName "schema" $Model.Singular}}.Relations,
{{- end}}
)
`
