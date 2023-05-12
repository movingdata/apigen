package main

import (
	"go/types"
	"io"
	"strings"
	"text/template"
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
		"github.com/satori/go.uuid",
		"movingdata.com/p/wbi/internal/modelutil",
		"movingdata.com/p/wbi/models/modelschema/" + strings.ToLower(typeName) + "schema",
	}
}

func (w *SQLWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	model, err := makeModel(typeName, namedType, structType)
	if err != nil {
		return err
	}
	return sqlTemplate.Execute(wr, *model)
}

var sqlTemplate = template.Must(template.New("sqlTemplate").Funcs(tplFunc).Parse(`
{{$Type := .}}

{{if $Type.HasSQLFindOne}}
// {{$Type.Singular}}SQLFindOne gets a single {{$Type.Singular}} record from the database according to a query
func {{$Type.Singular}}SQLFindOne(ctx context.Context, db modelutil.RowQueryerContext, fn func(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement) (*{{$Type.Singular}}, error) {
	qb := sqlbuilder.Select().From({{(PackageName "schema" $Type.Singular)}}.Table).Columns(modelutil.ColumnsAsExpressions({{(PackageName "schema" $Type.Singular)}}.Columns)...).OffsetLimit(sqlbuilder.OffsetLimit(sqlbuilder.Literal("0"), sqlbuilder.Literal("1")))

	if fn != nil {
		qb = fn(qb)
	}

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return nil, fmt.Errorf("{{$Type.Singular}}SQLFindOne: couldn't generate query: %w", err)
	}

	var m {{$Type.Singular}}
	if err := db.QueryRowContext(ctx, qs, qv...).Scan({{range $i, $Field := $Type.Fields}}{{if $Field.Array}}pq.Array(&m.{{$Field.GoName}}){{else}}&m.{{$Field.GoName}}{{end}}, {{end}}); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}

		return nil, fmt.Errorf("{{$Type.Singular}}SQLFindOne: couldn't perform query: %w", err)
	}

	return &m, nil
}
{{end}}

{{if $Type.HasSQLFindOneByID}}
// {{$Type.Singular}}SQLFindOneByID gets a single {{$Type.Singular}} record by its ID from the database
func {{$Type.Singular}}SQLFindOneByID(ctx context.Context, db modelutil.RowQueryerContext, id uuid.UUID) (*{{$Type.Singular}}, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("{{$Type.Singular}}SQLFindOneByID: id argument was empty")
	}

	v, err := {{$Type.Singular}}SQLFindOne(ctx, db, func(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement { return q.AndWhere(sqlbuilder.Eq({{(PackageName "schema" $Type.Singular)}}.ColumnID, sqlbuilder.Bind(id))) })
	if err != nil {
		return nil, fmt.Errorf("{{$Type.Singular}}SQLFindOneByID: couldn't get model: %w", err)
	}

	return v, nil
}
{{end}}

{{if $Type.HasSQLFindMultiple}}
// {{$Type.Singular}}SQLFindMultiple gets multiple {{$Type.Singular}} records from the database according to a query
func {{$Type.Singular}}SQLFindMultiple(ctx context.Context, db modelutil.QueryerContext, fn func(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement) ([]{{$Type.Singular}}, error) {
	qb := sqlbuilder.Select().From({{(PackageName "schema" $Type.Singular)}}.Table).Columns(modelutil.ColumnsAsExpressions({{(PackageName "schema" $Type.Singular)}}.Columns)...)

	if fn != nil {
		qb = fn(qb)
	}

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return nil, fmt.Errorf("{{$Type.Singular}}SQLFindMultiple: couldn't generate query: %w", err)
	}

	rows, err := db.QueryContext(ctx, qs, qv...)
	if err != nil {
		return nil, fmt.Errorf("{{$Type.Singular}}SQLFindMultiple: couldn't perform query: %w", err)
	}
	defer rows.Close()

	a := make([]{{$Type.Singular}}, 0)
	for rows.Next() {
		var m {{$Type.Singular}}
		if err := rows.Scan({{range $i, $Field := $Type.Fields}}&m.{{$Field.GoName}}, {{end}}); err != nil {
			return nil, fmt.Errorf("{{$Type.Singular}}SQLFindMultiple: couldn't scan row: %w", err)
		}

		a = append(a, m)
	}

	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("{{$Type.Singular}}SQLFindMultiple: couldn't close row set: %w", err)
	}

	return a, nil
}
{{end}}

{{if $Type.HasSQLCreate}}
// {{$Type.Singular}}SQLCreate creates a single {{$Type.Singular}} record in the database
func {{$Type.Singular}}SQLCreate(ctx context.Context, db modelutil.ExecerContext, userID uuid.UUID, now time.Time, m *{{$Type.Singular}}) error {
	if m.ID == uuid.Nil {
		return fmt.Errorf("{{$Type.Singular}}SQLCreate: ID field was empty")
	}

	qb := sqlbuilder.Insert().Table({{(PackageName "schema" $Type.Singular)}}.Table).Columns(sqlbuilder.InsertColumns{
{{- if $Type.HasID}}
		{{(PackageName "schema" $Type.Singular)}}.ColumnID: sqlbuilder.Bind(m.ID),
{{- end}}
{{- if $Type.HasCreatedAt}}
		{{(PackageName "schema" $Type.Singular)}}.ColumnCreatedAt: sqlbuilder.Bind(now),
{{- end}}
{{- if $Type.HasCreatorID}}
		{{(PackageName "schema" $Type.Singular)}}.ColumnCreatorID: sqlbuilder.Bind(userID),
{{- end}}
{{- if $Type.HasUpdatedAt}}
		{{(PackageName "schema" $Type.Singular)}}.ColumnUpdatedAt: sqlbuilder.Bind(now),
{{- end}}
{{- if $Type.HasUpdaterID}}
		{{(PackageName "schema" $Type.Singular)}}.ColumnUpdaterID: sqlbuilder.Bind(userID),
{{- end}}
{{- if $Type.HasVersion}}
		{{(PackageName "schema" $Type.Singular)}}.ColumnVersion: sqlbuilder.Bind(1),
{{- end}}
{{- range $Field := $Type.Fields}}
{{- if not $Field.IgnoreCreate}}
		{{(PackageName "schema" $Type.Singular)}}.Column{{$Field.GoName}}: sqlbuilder.Bind({{if $Field.Array}}pq.Array(m.{{$Field.GoName}}){{else}}m.{{$Field.GoName}}{{end}}),
{{- end}}
{{- end}}
	})

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return fmt.Errorf("{{$Type.Singular}}SQLCreate: couldn't generate query: %w", err)
	}

	if _, err := db.ExecContext(ctx, qs, qv...); err != nil {
		return fmt.Errorf("{{$Type.Singular}}SQLCreate: couldn't perform query: %w", err)
	}

	return nil
}
{{end}}

{{if $Type.HasSQLSave}}
// {{$Type.Singular}}SQLSave updates a single {{$Type.Singular}} record in the database
func {{$Type.Singular}}SQLSave(ctx context.Context, db interface { modelutil.RowQueryerContext; modelutil.ExecerContext }, userID uuid.UUID, now time.Time, m *{{$Type.Singular}}) error {
	if m.ID == uuid.Nil {
		return fmt.Errorf("{{$Type.Singular}}SQLSave: ID field was empty")
	}

	p, err := {{$Type.Singular}}SQLFindOneByID(ctx, db, m.ID)
	if err != nil {
		return fmt.Errorf("{{$Type.Singular}}SQLSave: couldn't fetch previous state: %w", err)
	}

{{- if $Type.HasUpdatedAt}}
	m.UpdatedAt = now
{{- end}}
{{- if $Type.HasUpdaterID}}
	m.UpdaterID = userID
{{- end}}
{{- if $Type.HasVersion}}
	if m.Version != p.Version {
		return fmt.Errorf("{{$Type.Singular}}SQLSave: Version from input did not match current state (input=%d current=%d): %w", m.Version, p.Version, ErrVersionMismatch)
	}
	m.Version = m.Version + 1
{{- end}}

	uc := sqlbuilder.UpdateColumns{
{{- if $Type.HasUpdatedAt}}
		{{(PackageName "schema" $Type.Singular)}}.ColumnUpdatedAt: sqlbuilder.Bind(m.UpdatedAt),
{{- end}}
{{- if $Type.HasUpdaterID}}
		{{(PackageName "schema" $Type.Singular)}}.ColumnUpdaterID: sqlbuilder.Bind(m.UpdaterID),
{{- end}}
{{- if $Type.HasVersion}}
		{{(PackageName "schema" $Type.Singular)}}.ColumnVersion: sqlbuilder.Bind(m.Version),
{{- end}}
	}

{{- range $Field := $Type.Fields}}
{{- if not $Field.IgnoreUpdate}}
	if {{ (NotEqual (Join "m." $Field.GoName) $Field.GoType (Join "p." $Field.GoName) $Field.GoType) }} {
		uc[{{(PackageName "schema" $Type.Singular)}}.Column{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(m.{{$Field.GoName}}){{else}}m.{{$Field.GoName}}{{end}})
	}
{{- end}}
{{- end}}

	qb := sqlbuilder.Update().Table({{(PackageName "schema" $Type.Singular)}}.Table).Set(uc).Where(sqlbuilder.Eq({{(PackageName "schema" $Type.Singular)}}.ColumnID, sqlbuilder.Bind(m.ID)))

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return fmt.Errorf("{{$Type.Singular}}SQLSave: couldn't generate query: %w", err)
	}

	if _, err := db.ExecContext(ctx, qs, qv...); err != nil {
		return fmt.Errorf("{{$Type.Singular}}SQLSave: couldn't update record: %w", err)
	}

	return nil
}
{{end}}
`))
