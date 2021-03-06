package main

import (
	"go/types"
	"io"
	"strings"
	"text/template"

	"github.com/pkg/errors"
)

type SQLWriter struct{ Dir string }

func NewSQLWriter(dir string) *SQLWriter { return &SQLWriter{Dir: dir} }

func (SQLWriter) Name() string     { return "sql" }
func (SQLWriter) Language() string { return "go" }
func (w SQLWriter) File(typeName string, _ *types.Named, _ *types.Struct) string {
	return w.Dir + "/" + strings.ToLower(typeName) + "_sql.go"
}

func (SQLWriter) Imports(typeName string, namedType *types.Named, structType *types.Struct) []string {
	return []string{
		"context",
		"database/sql",
		"time",
		"fknsrs.biz/p/sqlbuilder",
		"github.com/pkg/errors",
		"github.com/satori/go.uuid",
		"movingdata.com/p/wbi/internal/modelutil",
		"movingdata.com/p/wbi/models/modelschema/" + strings.ToLower(typeName) + "schema",
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
	d, err := getSQLTemplateData(typeName, namedType, structType)
	if err != nil {
		return err
	}

	return sqlTemplate.Execute(wr, d)
}

func getSQLTemplateData(typeName string, namedType *types.Named, structType *types.Struct) (*sqlTemplateData, error) {
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
		hasFindMultiple = false
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
			return nil, errors.Errorf("can't determine sql type for go type %q", ft)
		}

		if a, ok := sqlArgs["table"]; ok && len(a) > 0 {
			tableName = a[0][0]
		}

		if a, ok := sqlArgs["create"]; ok && len(a) > 0 {
			hasCreate = true
		}
		if a, ok := sqlArgs["findMultiple"]; ok && len(a) > 0 {
			hasFindMultiple = true
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

	return &sqlTemplateData{
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
		HasFindMultiple: hasFindMultiple,
		HasFindOne:      hasFindOne,
		HasFindOneByID:  hasFindOneByID,
		HasSave:         hasSave,
	}, nil
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
	HasFindMultiple bool
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

var sqlTemplate = template.Must(template.New("sqlTemplate").Funcs(tplFunc).Parse(`
{{$Root := .}}

{{if $Root.HasFindOne}}
// {{$Root.Name}}SQLFindOne gets a single {{$Root.Name}} record from the database according to a query
func {{$Root.Name}}SQLFindOne(ctx context.Context, db modelutil.RowQueryerContext, fn func(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement) (*{{$Root.Name}}, error) {
	qb := sqlbuilder.Select().From({{$Root.Name | LC}}schema.Table).Columns(modelutil.ColumnsAsExpressions({{$Root.Name | LC}}schema.Columns)...).OffsetLimit(sqlbuilder.OffsetLimit(sqlbuilder.Literal("0"), sqlbuilder.Literal("1")))

	if fn != nil {
		qb = fn(qb)
	}

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return nil, errors.Wrap(err, "{{$Root.Name}}SQLFindOne: couldn't generate query")
	}

	var m {{$Root.Name}}
	if err := db.QueryRowContext(ctx, qs, qv...).Scan({{range $i, $Field := $Root.Fields}}{{if $Field.Array}}pq.Array(&m.{{$Field.GoName}}){{else}}&m.{{$Field.GoName}}{{end}}, {{end}}); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}

		return nil, errors.Wrap(err, "{{$Root.Name}}SQLFindOne: couldn't perform query")
	}

	return &m, nil
}
{{end}}

{{if $Root.HasFindOneByID}}
// {{$Root.Name}}SQLFindOneByID gets a single {{$Root.Name}} record by its ID from the database
func {{$Root.Name}}SQLFindOneByID(ctx context.Context, db modelutil.RowQueryerContext, id uuid.UUID) (*{{$Root.Name}}, error) {
	if !modelutil.Truthy(id) {
		return nil, errors.Errorf("{{$Root.Name}}SQLFindOneByID: id argument was empty")
	}

	v, err := {{$Root.Name}}SQLFindOne(ctx, db, func(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement { return q.AndWhere(sqlbuilder.Eq({{$Root.Name | LC}}schema.ColumnID, sqlbuilder.Bind(id))) })
	if err != nil {
		return nil, errors.Wrap(err, "{{$Root.Name}}SQLFindOneByID: couldn't get model")
	}

	return v, nil
}
{{end}}

{{if $Root.HasFindMultiple}}
// {{$Root.Name}}SQLFindMultiple gets multiple {{$Root.Name}} records from the database according to a query
func {{$Root.Name}}SQLFindMultiple(ctx context.Context, db modelutil.QueryerContext, fn func(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement) ([]{{$Root.Name}}, error) {
	qb := sqlbuilder.Select().From({{$Root.Name | LC}}schema.Table).Columns(modelutil.ColumnsAsExpressions({{$Root.Name | LC}}schema.Columns)...)

	if fn != nil {
		qb = fn(qb)
	}

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return nil, errors.Wrap(err, "{{$Root.Name}}SQLFindMultiple: couldn't generate query")
	}

	rows, err := db.QueryContext(ctx, qs, qv...)
	if err != nil {
		return nil, errors.Wrap(err, "{{$Root.Name}}SQLFindMultiple: couldn't perform query")
	}
	defer rows.Close()

	a := make([]{{$Root.Name}}, 0)
	for rows.Next() {
		var m {{$Root.Name}}
		if err := rows.Scan({{range $i, $Field := $Root.Fields}}&m.{{$Field.GoName}}, {{end}}); err != nil {
			return nil, errors.Wrap(err, "{{$Root.Name}}SQLFindMultiple: couldn't scan row")
		}

		a = append(a, m)
	}

	if err := rows.Close(); err != nil {
		return nil, errors.Wrap(err, "{{$Root.Name}}SQLFindMultiple: couldn't close row set")
	}

	return a, nil
}
{{end}}

{{if (and $Root.CanCreate $Root.HasCreate)}}
// {{$Root.Name}}SQLCreate creates a single {{$Root.Name}} record in the database
func {{$Root.Name}}SQLCreate(ctx context.Context, db modelutil.ExecerContext, userID uuid.UUID, now time.Time, m *{{$Root.Name}}) error {
	if !modelutil.Truthy(m.ID) {
		return errors.Errorf("{{$Root.Name}}SQLCreate: ID field was empty")
	}

	qb := sqlbuilder.Insert().Table({{$Root.Name | LC}}schema.Table).Columns(sqlbuilder.InsertColumns{
		{{$Root.Name | LC}}schema.ColumnID: sqlbuilder.Bind(m.ID),
{{if $Root.HasCreatedAt -}}
		{{$Root.Name | LC}}schema.ColumnCreatedAt: sqlbuilder.Bind(now),
{{- end}}
{{if $Root.HasCreatorID -}}
		{{$Root.Name | LC}}schema.ColumnCreatorID: sqlbuilder.Bind(userID),
{{- end}}
{{if $Root.HasUpdatedAt -}}
		{{$Root.Name | LC}}schema.ColumnUpdatedAt: sqlbuilder.Bind(now),
{{- end}}
{{if $Root.HasUpdaterID -}}
		{{$Root.Name | LC}}schema.ColumnUpdaterID: sqlbuilder.Bind(userID),
{{- end}}
{{- range $Field := $Root.CreateFields}}
		{{$Root.Name | LC}}schema.Column{{$Field.GoName}}: sqlbuilder.Bind({{if $Field.Array}}pq.Array(m.{{$Field.GoName}}){{else}}m.{{$Field.GoName}}{{end}}),
{{- end}}
	})

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return errors.Wrap(err, "{{$Root.Name}}SQLCreate: couldn't generate query")
	}

	if _, err := db.ExecContext(ctx, qs, qv...); err != nil {
		return errors.Wrap(err, "{{$Root.Name}}SQLCreate: couldn't perform query")
	}

	return nil
}
{{end}}

{{if (and $Root.CanUpdate $Root.HasSave)}}
// {{$Root.Name}}SQLSave updates a single {{$Root.Name}} record in the database
func {{$Root.Name}}SQLSave(ctx context.Context, db interface { modelutil.RowQueryerContext; modelutil.ExecerContext }, userID uuid.UUID, now time.Time, m *{{$Root.Name}}) error {
	if !modelutil.Truthy(m.ID) {
		return errors.Errorf("{{$Root.Name}}SQLSave: ID field was empty")
	}

	p, err := {{$Root.Name}}SQLFindOneByID(ctx, db, m.ID)
	if err != nil {
		return errors.Wrap(err, "{{$Root.Name}}SQLSave: couldn't fetch previous state")
	}

{{if $Root.HasUpdatedAt -}}
	m.UpdatedAt = now
{{- end}}
{{if $Root.HasUpdaterID -}}
	m.UpdaterID = userID
{{- end}}

	uc := sqlbuilder.UpdateColumns{
{{if $Root.HasUpdatedAt -}}
		{{$Root.Name | LC}}schema.ColumnUpdatedAt: sqlbuilder.Bind(m.UpdatedAt),
{{- end}}
{{if $Root.HasUpdaterID -}}
		{{$Root.Name | LC}}schema.ColumnUpdaterID: sqlbuilder.Bind(m.UpdaterID),
{{- end}}
	}

{{- range $Field := $Root.UpdateFields}}
	if !modelutil.Compare(m.{{$Field.GoName}}, p.{{$Field.GoName}}) {
		uc[{{$Root.Name | LC}}schema.Column{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(m.{{$Field.GoName}}){{else}}m.{{$Field.GoName}}{{end}})
	}
{{- end}}

	qb := sqlbuilder.Update().Table({{$Root.Name | LC}}schema.Table).Set(uc).Where(sqlbuilder.Eq({{$Root.Name | LC}}schema.ColumnID, sqlbuilder.Bind(m.ID)))

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return errors.Wrap(err, "{{$Root.Name}}SQLSave: couldn't generate query")
	}

	if _, err := db.ExecContext(ctx, qs, qv...); err != nil {
		return errors.Wrap(err, "{{$Root.Name}}SQLSave: couldn't update record")
	}

	return nil
}
{{end}}
`))
