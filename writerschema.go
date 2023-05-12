package main

import (
	"bytes"
	"fmt"
	"go/types"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"
	"text/template"
)

type SchemaWriter struct {
	dir  string
	pkgs []string
}

func NewSchemaWriter(dir string) *SchemaWriter { return &SchemaWriter{dir: dir} }

func (SchemaWriter) Name() string     { return "schema" }
func (SchemaWriter) Language() string { return "go" }
func (w SchemaWriter) File(typeName string, _ *types.Named, _ *types.Struct) string {
	return w.dir + "/modelschema/" + strings.ToLower(typeName) + "schema/" + strings.ToLower(typeName) + "schema.go"
}

func (w SchemaWriter) PackageName(typeName string, _ *types.Named, _ *types.Struct) string {
	return strings.ToLower(typeName) + "schema"
}

func (SchemaWriter) Imports(typeName string, namedType *types.Named, structType *types.Struct) []string {
	return []string{
		"movingdata.com/p/wbi/internal/apitypes",
		"fknsrs.biz/p/sqlbuilder",
	}
}

func (w *SchemaWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	w.pkgs = append(w.pkgs, w.PackageName(typeName, namedType, structType))

	model, err := makeModel(typeName, namedType, structType)
	if err != nil {
		return err
	}
	return schemaTemplate.Execute(wr, *model)
}

func (w *SchemaWriter) Finish(dry bool) error {
	buf := bytes.NewBuffer(nil)

	fmt.Fprintf(buf, "package modelschema\n\n")
	fmt.Fprintf(buf, "import (\n")
	fmt.Fprintf(buf, "  \"movingdata.com/p/wbi/internal/apitypes\"\n")
	for _, pkg := range w.pkgs {
		fmt.Fprintf(buf, "  %q\n", "movingdata.com/p/wbi/models/modelschema/"+pkg)
	}
	fmt.Fprintf(buf, ")\n\n")
	fmt.Fprintf(buf, "var Tables = map[string]*sqlbuilder.Table{\n")
	for _, pkg := range w.pkgs {
		fmt.Fprintf(buf, "  %s.Model.GoName: %s.Table,\n", filepath.Base(pkg), filepath.Base(pkg))
		fmt.Fprintf(buf, "  %s.Model.SQLName: %s.Table,\n", filepath.Base(pkg), filepath.Base(pkg))
	}
	fmt.Fprintf(buf, "}\n\n")
	fmt.Fprintf(buf, "var Models = map[string]*apitypes.Model{\n")
	for _, pkg := range w.pkgs {
		fmt.Fprintf(buf, "  %s.Model.GoName: %s.Model,\n", filepath.Base(pkg), filepath.Base(pkg))
	}
	fmt.Fprintf(buf, "}\n\n")
	fmt.Fprintf(buf, "var Relations = flattenRelations(\n")
	for _, pkg := range w.pkgs {
		fmt.Fprintf(buf, "  %s.Relations,\n", filepath.Base(pkg))
	}
	fmt.Fprintf(buf, ")\n")

	if !dry {
		if err := ioutil.WriteFile(w.dir+"/modelschema/models.go", buf.Bytes(), 0644); err != nil {
			return err
		}
	}

	return nil
}

var schemaTemplate = template.Must(template.New("schemaTemplate").Funcs(tplFunc).Parse(`
{{$Type := .}}

// Table is a symbolic identifier for the "{{$Type.SQLTableName}}" table
var Table = sqlbuilder.NewTable(
	"{{$Type.SQLTableName}}",
{{- range $Field := $Type.Fields}}
	"{{$Field.SQLName}}",
{{- end}}
)

var (
{{- range $Field := $Type.Fields}}
	// Column{{$Field.GoName}} is a symbolic identifier for the "{{$Type.SQLTableName}}"."{{$Field.SQLName}}" column
	Column{{$Field.GoName}} = Table.C("{{$Field.SQLName}}")
{{- end}}
)

// Columns is a list of columns in the "{{$Type.SQLTableName}}" table
var Columns = []*sqlbuilder.BasicColumn{
{{- range $Field := $Type.Fields}}
	Column{{$Field.GoName}},
{{- end}}
}

var Model = &apitypes.Model{
	GoName: "{{$Type.Singular}}",
	SQLName: "{{$Type.SQLTableName}}",
	APIName: "{{$Type.LowerPlural}}",
	Fields: []*apitypes.Field{
{{- range $Field := $Type.Fields}}
		&apitypes.Field{
			GoName: "{{$Field.GoName}}",
			GoType: "{{$Field.GoType}}",
			SQLName: "{{$Field.SQLName}}",
			SQLType: "{{$Field.SQLType}}",
			APIName: "{{$Field.APIName}}",
			APIType: "{{$Field.JSType}}",
			Array: {{if $Field.Array}}true{{else}}false{{end}},
			NotNull: {{if $Field.IsNull}}false{{else}}true{{end}},
		},
{{- end}}
	},
}

var Relations = []*apitypes.Relation{
{{- range $Field := $Type.Fields}}
{{- range $Ref := $Field.APIRefs}}
	&apitypes.Relation{
		SourceModel: "{{$Type.Singular}}",
		SourceField: "{{$Field.GoName}}",
		TargetModel: "{{$Ref.ModelName}}",
		TargetField: "{{$Ref.FieldName}}",
	},
{{- end}}
{{- end}}
}
`))
