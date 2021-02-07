package main

import (
	"go/types"
	"bytes"
	"fmt"
	"io/ioutil"
	"io"
	"strings"
	"text/template"

	"github.com/pkg/errors"
)

type SQLWriter struct{ dir string; pkgs []string }

func NewSQLWriter(dir string) *SQLWriter { return &SQLWriter{dir: dir, pkgs: []string{}} }

func (SQLWriter) Name() string     { return "sql" }
func (SQLWriter) Language() string { return "go" }
func (w SQLWriter) File(typeName string, _ *types.Named, _ *types.Struct) string {
	return w.dir + "/modelsql/" + strings.ToLower(typeName) + "sql/" + strings.ToLower(typeName) + "sql.go"
}

func (w SQLWriter) PackageName(typeName string, _ *types.Named, _ *types.Struct) string {
  return strings.ToLower(typeName) + "sql"
}

func (SQLWriter) Imports() []string {
	return []string{
		"context",
		"database/sql",
		"time",
		"fknsrs.biz/p/sqlbuilder",
		"github.com/pkg/errors",
		"github.com/satori/go.uuid",
		"movingdata.com/p/wbi/internal/modelutil",
		"movingdata.com/p/wbi/models",
	}
}

var sqlIgnoreInput = map[string]bool{
	"id":         true,
	"created_at": true,
	"updated_at": true,
	"creator_id": true,
	"updater_id": true,
}

var sqlTypes = map[string]string{
	"bool":                           "boolean",
	"encoding/json.RawMessage":       "json",
	"float64":                        "double precision",
	"github.com/satori/go.uuid.UUID": "uuid",
	"int":                            "integer",
	"int64":                          "integer",
	"fknsrs.biz/p/civil.Date":        "date",
	"string":                         "text",
	"time.Time":                      "timestamp with time zone",
}

func (w *SQLWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	w.pkgs = append(w.pkgs, w.PackageName(typeName, namedType, structType))

	pluralSnake, _ := pluralFor(namedType.Obj().Name())
	_, singularCamel := singularFor(namedType.Obj().Name())

	var (
		fields          []sqlField
		createFields    []sqlField
		updateFields    []sqlField
		canCreate       = false
		canUpdate       = false
		hasCreatedAt    = false
		hasUpdatedAt    = false
		hasCreatorID    = false
		hasUpdaterID    = false
		hasCreate       = false
		hasFindOne      = false
		hasFindOneByID  = false
		hasSave         = false
		tableName       = pluralSnake
	)

	for i := 0; i < structType.NumFields(); i++ {
		fld := structType.Field(i)
		if !fld.Exported() {
			continue
		}

		ft := fld.Type()

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

		sqlName, sqlArgs := getAndParseTag(structType, fld.Name(), "sql")
		if sqlName == "-" {
			continue
		} else if sqlName == "" {
			sqlName = ucls.String(fld.Name())
		}

		sqlType, ok := sqlTypes[strings.TrimPrefix(ft.String(), "movingdata.com/p/wbi/vendor/")]
		if !ok {
			return errors.Errorf("can't determine sql type for go type %q (field %q)", ft, fld.Name())
		}

		if a, ok := sqlArgs["table"]; ok && len(a) > 0 {
			tableName = a[0][0]
		}

		if a, ok := sqlArgs["create"]; ok && len(a) > 0 {
			hasCreate = true
		}
		if a, ok := sqlArgs["findOne"]; ok && len(a) > 0 {
			hasFindOne = true
		}
		if a, ok := sqlArgs["findOneByID"]; ok && len(a) > 0 {
			hasFindOneByID = true
		}
		if a, ok := sqlArgs["save"]; ok && len(a) > 0 {
			hasSave = true
		}

		if sqlName == "created_at" {
			hasCreatedAt = true
			canCreate = true
		}
		if sqlName == "updated_at" {
			hasUpdatedAt = true
			canUpdate = true
		}
		if sqlName == "creator_id" {
			hasCreatorID = true
		}
		if sqlName == "updater_id" {
			hasUpdaterID = true
		}

		f := sqlField{
			GoName:  fld.Name(),
			SQLName: sqlName,
			SQLType: sqlType,
			Array:   isSlice,
			Pointer: isPointer,
		}

		fields = append(fields, f)
		if !sqlIgnoreInput[sqlName] {
			createFields = append(createFields, f)
			updateFields = append(updateFields, f)
		}
	}

	return sqlTemplate.Execute(wr, sqlTemplateData{
		Name:            namedType.Obj().Name(),
		TableName:       tableName,
		PluralSnake:     pluralSnake,
		SingularCamel:   singularCamel,
		Fields:          fields,
		CreateFields:    createFields,
		UpdateFields:    updateFields,
		CanCreate:       canCreate,
		CanUpdate:       canUpdate,
		HasCreatedAt:    hasCreatedAt,
		HasUpdatedAt:    hasUpdatedAt,
		HasCreatorID:    hasCreatorID,
		HasUpdaterID:    hasUpdaterID,
		HasCreate:       hasCreate,
		HasFindOne:      hasFindOne,
		HasFindOneByID:  hasFindOneByID,
		HasSave:         hasSave,
	})
}

type sqlTemplateData struct {
	Name            string
	TableName       string
	PluralSnake     string
	SingularCamel   string
	Fields          []sqlField
	CreateFields    []sqlField
	UpdateFields    []sqlField
	CanCreate       bool
	CanUpdate       bool
	HasCreatedAt    bool
	HasUpdatedAt    bool
	HasCreatorID    bool
	HasUpdaterID    bool
	HasCreate       bool
	HasFindOne      bool
	HasFindOneByID  bool
	HasSave         bool
}

type sqlField struct {
	GoName  string
	SQLName string
	SQLType string
	Array   bool
	Pointer bool
}

var sqlTemplate = template.Must(template.New("sqlTemplate").Parse(`
{{$Root := .}}

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

{{if $Root.HasFindOne}}
// FindOne gets a single models.{{$Root.Name}} record from the database according to a query
func FindOne(ctx context.Context, db modelutil.RowQueryerContext, fn func(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement) (*models.{{$Root.Name}}, error) {
	qb := sqlbuilder.Select().From(Table).Columns(modelutil.ColumnsAsExpressions(Columns)...).OffsetLimit(sqlbuilder.OffsetLimit(sqlbuilder.Literal("0"), sqlbuilder.Literal("1")))

	if fn != nil {
		qb = fn(qb)
	}

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return nil, errors.Wrap(err, "FindOne: couldn't generate query")
	}

	var m models.{{$Root.Name}}
	if err := db.QueryRowContext(ctx, qs, qv...).Scan({{range $i, $Field := $Root.Fields}}{{if $Field.Array}}pq.Array(&m.{{$Field.GoName}}){{else}}&m.{{$Field.GoName}}{{end}}, {{end}}); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}

		return nil, errors.Wrap(err, "FindOne: couldn't perform query")
	}

	return &m, nil
}
{{end}}

{{if $Root.HasFindOneByID}}
// FindOneByID gets a single models.{{$Root.Name}} record by its ID from the database
func FindOneByID(ctx context.Context, db modelutil.RowQueryerContext, id uuid.UUID) (*models.{{$Root.Name}}, error) {
	if !modelutil.Truthy(id) {
		return nil, errors.Errorf("FindOneByID: id argument was empty")
	}

	v, err := FindOne(ctx, db, func(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement { return q.AndWhere(sqlbuilder.Eq(ColumnID, sqlbuilder.Bind(id))) })
	if err != nil {
		return nil, errors.Wrap(err, "FindOneByID: couldn't get model")
	}

	return v, nil
}
{{end}}

{{if (and $Root.CanCreate $Root.HasCreate)}}
// Create creates a single models.{{$Root.Name}} record in the database
func Create(ctx context.Context, db modelutil.ExecerContext, userID uuid.UUID, now time.Time, m *models.{{$Root.Name}}) error {
	if !modelutil.Truthy(m.ID) {
		return errors.Errorf("Create: ID field was empty")
	}

	qb := sqlbuilder.Insert().Table(Table).Columns(sqlbuilder.InsertColumns{
		ColumnID: sqlbuilder.Bind(m.ID),
{{if $Root.HasCreatedAt -}}
		ColumnCreatedAt: sqlbuilder.Bind(now),
{{- end}}
{{if $Root.HasCreatorID -}}
		ColumnCreatorID: sqlbuilder.Bind(userID),
{{- end}}
{{if $Root.HasUpdatedAt -}}
		ColumnUpdatedAt: sqlbuilder.Bind(now),
{{- end}}
{{if $Root.HasUpdaterID -}}
		ColumnUpdaterID: sqlbuilder.Bind(userID),
{{- end}}
{{- range $Field := $Root.CreateFields}}
		Column{{$Field.GoName}}: sqlbuilder.Bind({{if $Field.Array}}pq.Array(m.{{$Field.GoName}}){{else}}m.{{$Field.GoName}}{{end}}),
{{- end}}
	})

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return errors.Wrap(err, "Create: couldn't generate query")
	}

	if _, err := db.ExecContext(ctx, qs, qv...); err != nil {
		return errors.Wrap(err, "Create: couldn't perform query")
	}

	return nil
}
{{end}}

{{if (and $Root.CanUpdate $Root.HasSave)}}
// Save updates a single models.{{$Root.Name}} record in the database
func Save(ctx context.Context, db interface { modelutil.RowQueryerContext; modelutil.ExecerContext }, userID uuid.UUID, now time.Time, m *models.{{$Root.Name}}) error {
	if !modelutil.Truthy(m.ID) {
		return errors.Errorf("Save: ID field was empty")
	}

	p, err := FindOneByID(ctx, db, m.ID)
	if err != nil {
		return errors.Wrap(err, "Save: couldn't fetch previous state")
	}

{{if $Root.HasUpdatedAt -}}
	m.UpdatedAt = now
{{- end}}
{{if $Root.HasUpdaterID -}}
	m.UpdaterID = userID
{{- end}}

	uc := sqlbuilder.UpdateColumns{
{{if $Root.HasUpdatedAt -}}
		ColumnUpdatedAt: sqlbuilder.Bind(m.UpdatedAt),
{{- end}}
{{if $Root.HasUpdaterID -}}
		ColumnUpdaterID: sqlbuilder.Bind(m.UpdaterID),
{{- end}}
	}

{{- range $Field := $Root.UpdateFields}}
	if !modelutil.Compare(m.{{$Field.GoName}}, p.{{$Field.GoName}}) {
		uc[Column{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(m.{{$Field.GoName}}){{else}}m.{{$Field.GoName}}{{end}})
	}
{{- end}}

	qb := sqlbuilder.Update().Table(Table).Set(uc).Where(sqlbuilder.Eq(ColumnID, sqlbuilder.Bind(m.ID)))

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return errors.Wrap(err, "Save: couldn't generate query")
	}

	if _, err := db.ExecContext(ctx, qs, qv...); err != nil {
		return errors.Wrap(err, "Save: couldn't update record")
	}

	return nil
}
{{end}}
`))

func (w *SQLWriter) Finish(dry bool) error {
	buf := bytes.NewBuffer(nil)

	fmt.Fprintf(buf, "package modelsql\n\n")
	fmt.Fprintf(buf, "import (\n")
	for _, pkg := range w.pkgs {
		fmt.Fprintf(buf, "  %q\n", "movingdata.com/p/wbi/models/modelsql/" + pkg)
	}
	fmt.Fprintf(buf, ")\n\n")

	fmt.Fprintf(buf, "func init() {\n")
	for _, pkg := range w.pkgs {
		fmt.Fprintf(buf, "  registerTable(%s.Table)\n", pkg)
	}
	fmt.Fprintf(buf, "}\n")

	if !dry {
		if err := ioutil.WriteFile(w.dir + "/modelsql/tables.go", buf.Bytes(), 0644); err != nil {
			return err
		}
	}

	return nil
}
