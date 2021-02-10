package main

import (
	"go/types"
	"fmt"
	"bytes"
	"io/ioutil"
	"io"
	"strings"
	"text/template"
	"path/filepath"
)

type SchemaWriter struct{ dir string; pkgs []string }

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
		"fknsrs.biz/p/sqlbuilder",
	}
}

func (w *SchemaWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	d, err := getSQLTemplateData(typeName,namedType,structType)
	if err != nil {
		return err
	}

	w.pkgs = append(w.pkgs, w.PackageName(typeName, namedType,structType))

	return schemaTemplate.Execute(wr, d)
}

func (w *SchemaWriter) Finish(dry bool) error {
	buf := bytes.NewBuffer(nil)

	fmt.Fprintf(buf, "package modelschema\n\n")
	fmt.Fprintf(buf, "import (\n")
	fmt.Fprintf(buf, "  \"movingdata.com/p/wbi/internal/apitypes\"\n")
	for _, pkg := range w.pkgs {
		fmt.Fprintf(buf, "  %q\n", "movingdata.com/p/wbi/models/modelschema/" + pkg)
	}
	fmt.Fprintf(buf, ")\n\n")
	fmt.Fprintf(buf, "var Tables = map[string]apitypes.Table{\n")
	for _, pkg := range w.pkgs {
		fmt.Fprintf(buf, "  %s.Schema.Name: %s.Schema,\n", filepath.Base(pkg), filepath.Base(pkg))
	}
	fmt.Fprintf(buf, "}\n")

	if !dry {
		if err := ioutil.WriteFile(w.dir + "/modelschema/tables.go", buf.Bytes(), 0644); err != nil {
			return err
		}
	}

	return nil
}

var schemaTemplate = template.Must(template.New("schemaTemplate").Funcs(tplFunc).Parse(`
{{$Root := .}}

// Table is a symbolic identifier for the "{{$Root.TableName}}" table
var Table = sqlbuilder.NewTable(
	"{{$Root.TableName}}",
{{- range $Field := $Root.Fields}}
	"{{$Field.SQLName}}",
{{- end}}
)

var (
{{- range $Field := $Root.Fields}}
	// Column{{$Field.GoName}} is a symbolic identifier for the "{{$Root.TableName}}"."{{$Field.SQLName}}" column
	Column{{$Field.GoName}} = Table.C("{{$Field.SQLName}}")
{{- end}}
)

// Columns is a list of columns in the "{{$Root.TableName}}" table
var Columns = []*sqlbuilder.BasicColumn{
{{- range $Field := $Root.Fields}}
	Column{{$Field.GoName}},
{{- end}}
}

var Schema = apitypes.Table{
	Name: "{{$Root.TableName}}",
	Fields: []apitypes.TableField{
{{- range $Field := $Root.Fields}}
		apitypes.TableField{
			Name: "{{$Field.SQLName}}",
			Type: "{{$Field.SQLType}}",
			Array: {{if $Field.Array}}true{{else}}false{{end}},
			NotNull: {{if $Field.Pointer}}false{{else}}true{{end}},
		},
{{- end}}
	},
}
`))
