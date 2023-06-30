package main

import (
	"strings"
)

type SQLGenerator struct {
	dir string
}

func NewSQLGenerator(dir string) *SQLGenerator {
	return &SQLGenerator{dir: dir}
}

func (g *SQLGenerator) Name() string {
	return "sql"
}

func (g *SQLGenerator) Model(model *Model) []writer {
	return []writer{
		&basicWriterForGo{
			basicWriter: basicWriter{
				name:     "individual",
				language: "go",
				file:     g.dir + "/" + strings.ToLower(model.Singular) + "_sql.go",
				write:    templateWriter(sqlTemplate, map[string]interface{}{"Model": model}),
			},
			packageName: "models",
			imports: []string{
				"context",
				"database/sql",
				"time",
				"fknsrs.biz/p/sqlbuilder",
				"github.com/satori/go.uuid",
				"movingdata.com/p/wbi/internal/modelutil",
				"movingdata.com/p/wbi/models/modelschema/" + strings.ToLower(model.Singular) + "schema",
			},
		},
	}
}

var sqlTemplate = `
{{$Model := .Model}}

{{if $Model.HasSQLFindOne}}
// {{$Model.Singular}}SQLFindOne gets a single {{$Model.Singular}} record from the database according to a query
func {{$Model.Singular}}SQLFindOne(ctx context.Context, db modelutil.RowQueryerContext, fn func(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement) (*{{$Model.Singular}}, error) {
	qb := sqlbuilder.Select().From({{(PackageName "schema" $Model.Singular)}}.Table).Columns(modelutil.ColumnsAsExpressions({{(PackageName "schema" $Model.Singular)}}.Columns)...).OffsetLimit(sqlbuilder.OffsetLimit(sqlbuilder.Literal("0"), sqlbuilder.Literal("1")))

	if fn != nil {
		qb = fn(qb)
	}

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return nil, fmt.Errorf("{{$Model.Singular}}SQLFindOne: couldn't generate query: %w", err)
	}

	var m {{$Model.Singular}}
	if err := db.QueryRowContext(ctx, qs, qv...).Scan({{range $i, $Field := $Model.Fields}}{{if $Field.Array}}pq.Array(&m.{{$Field.GoName}}){{else}}&m.{{$Field.GoName}}{{end}}, {{end}}); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}

		return nil, fmt.Errorf("{{$Model.Singular}}SQLFindOne: couldn't perform query: %w", err)
	}

	return &m, nil
}
{{end}}

{{if $Model.HasSQLFindOneByID}}
// {{$Model.Singular}}SQLFindOneByID gets a single {{$Model.Singular}} record by its ID from the database
func {{$Model.Singular}}SQLFindOneByID(ctx context.Context, db modelutil.RowQueryerContext, id uuid.UUID) (*{{$Model.Singular}}, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("{{$Model.Singular}}SQLFindOneByID: id argument was empty")
	}

	v, err := {{$Model.Singular}}SQLFindOne(ctx, db, func(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement { return q.AndWhere(sqlbuilder.Eq({{(PackageName "schema" $Model.Singular)}}.ColumnID, sqlbuilder.Bind(id))) })
	if err != nil {
		return nil, fmt.Errorf("{{$Model.Singular}}SQLFindOneByID: couldn't get model: %w", err)
	}

	return v, nil
}
{{end}}

{{if $Model.HasSQLFindMultiple}}
// {{$Model.Singular}}SQLFindMultiple gets multiple {{$Model.Singular}} records from the database according to a query
func {{$Model.Singular}}SQLFindMultiple(ctx context.Context, db modelutil.QueryerContext, fn func(q *sqlbuilder.SelectStatement) *sqlbuilder.SelectStatement) ([]{{$Model.Singular}}, error) {
	qb := sqlbuilder.Select().From({{(PackageName "schema" $Model.Singular)}}.Table).Columns(modelutil.ColumnsAsExpressions({{(PackageName "schema" $Model.Singular)}}.Columns)...)

	if fn != nil {
		qb = fn(qb)
	}

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return nil, fmt.Errorf("{{$Model.Singular}}SQLFindMultiple: couldn't generate query: %w", err)
	}

	rows, err := db.QueryContext(ctx, qs, qv...)
	if err != nil {
		return nil, fmt.Errorf("{{$Model.Singular}}SQLFindMultiple: couldn't perform query: %w", err)
	}
	defer rows.Close()

	a := make([]{{$Model.Singular}}, 0)
	for rows.Next() {
		var m {{$Model.Singular}}
		if err := rows.Scan({{range $i, $Field := $Model.Fields}}&m.{{$Field.GoName}}, {{end}}); err != nil {
			return nil, fmt.Errorf("{{$Model.Singular}}SQLFindMultiple: couldn't scan row: %w", err)
		}

		a = append(a, m)
	}

	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("{{$Model.Singular}}SQLFindMultiple: couldn't close row set: %w", err)
	}

	return a, nil
}
{{end}}

{{if $Model.HasSQLCreate}}
// {{$Model.Singular}}SQLCreate creates a single {{$Model.Singular}} record in the database
func {{$Model.Singular}}SQLCreate(ctx context.Context, db modelutil.ExecerContext, userID uuid.UUID, now time.Time, m *{{$Model.Singular}}) error {
	if m.ID == uuid.Nil {
		return fmt.Errorf("{{$Model.Singular}}SQLCreate: ID field was empty")
	}

	qb := sqlbuilder.Insert().Table({{(PackageName "schema" $Model.Singular)}}.Table).Columns(sqlbuilder.InsertColumns{
{{- if $Model.HasID}}
		{{(PackageName "schema" $Model.Singular)}}.ColumnID: sqlbuilder.Bind(m.ID),
{{- end}}
{{- if $Model.HasCreatedAt}}
		{{(PackageName "schema" $Model.Singular)}}.ColumnCreatedAt: sqlbuilder.Bind(now),
{{- end}}
{{- if $Model.HasCreatorID}}
		{{(PackageName "schema" $Model.Singular)}}.ColumnCreatorID: sqlbuilder.Bind(userID),
{{- end}}
{{- if $Model.HasUpdatedAt}}
		{{(PackageName "schema" $Model.Singular)}}.ColumnUpdatedAt: sqlbuilder.Bind(now),
{{- end}}
{{- if $Model.HasUpdaterID}}
		{{(PackageName "schema" $Model.Singular)}}.ColumnUpdaterID: sqlbuilder.Bind(userID),
{{- end}}
{{- if $Model.HasVersion}}
		{{(PackageName "schema" $Model.Singular)}}.ColumnVersion: sqlbuilder.Bind(1),
{{- end}}
{{- range $Field := $Model.Fields}}
{{- if not $Field.IgnoreCreate}}
		{{(PackageName "schema" $Model.Singular)}}.Column{{$Field.GoName}}: sqlbuilder.Bind({{if $Field.Array}}pq.Array(m.{{$Field.GoName}}){{else}}m.{{$Field.GoName}}{{end}}),
{{- end}}
{{- end}}
	})

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return fmt.Errorf("{{$Model.Singular}}SQLCreate: couldn't generate query: %w", err)
	}

	if _, err := db.ExecContext(ctx, qs, qv...); err != nil {
		return fmt.Errorf("{{$Model.Singular}}SQLCreate: couldn't perform query: %w", err)
	}

	return nil
}
{{end}}

{{if $Model.HasSQLSave}}
// {{$Model.Singular}}SQLSave updates a single {{$Model.Singular}} record in the database
func {{$Model.Singular}}SQLSave(ctx context.Context, db interface { modelutil.RowQueryerContext; modelutil.ExecerContext }, userID uuid.UUID, now time.Time, m *{{$Model.Singular}}) error {
	if m.ID == uuid.Nil {
		return fmt.Errorf("{{$Model.Singular}}SQLSave: ID field was empty")
	}

	p, err := {{$Model.Singular}}SQLFindOneByID(ctx, db, m.ID)
	if err != nil {
		return fmt.Errorf("{{$Model.Singular}}SQLSave: couldn't fetch previous state: %w", err)
	}

{{- if $Model.HasUpdatedAt}}
	m.UpdatedAt = now
{{- end}}
{{- if $Model.HasUpdaterID}}
	m.UpdaterID = userID
{{- end}}
{{- if $Model.HasVersion}}
	if m.Version != p.Version {
		return fmt.Errorf("{{$Model.Singular}}SQLSave: Version from input did not match current state (input=%d current=%d): %w", m.Version, p.Version, ErrVersionMismatch)
	}
	m.Version = m.Version + 1
{{- end}}

	uc := sqlbuilder.UpdateColumns{
{{- if $Model.HasUpdatedAt}}
		{{(PackageName "schema" $Model.Singular)}}.ColumnUpdatedAt: sqlbuilder.Bind(m.UpdatedAt),
{{- end}}
{{- if $Model.HasUpdaterID}}
		{{(PackageName "schema" $Model.Singular)}}.ColumnUpdaterID: sqlbuilder.Bind(m.UpdaterID),
{{- end}}
{{- if $Model.HasVersion}}
		{{(PackageName "schema" $Model.Singular)}}.ColumnVersion: sqlbuilder.Bind(m.Version),
{{- end}}
	}

{{- range $Field := $Model.Fields}}
{{- if not $Field.IgnoreUpdate}}
	if {{ (NotEqual (Join "m." $Field.GoName) $Field.GoType (Join "p." $Field.GoName) $Field.GoType) }} {
		uc[{{(PackageName "schema" $Model.Singular)}}.Column{{$Field.GoName}}] = sqlbuilder.Bind({{if $Field.Array}}pq.Array(m.{{$Field.GoName}}){{else}}m.{{$Field.GoName}}{{end}})
	}
{{- end}}
{{- end}}

	qb := sqlbuilder.Update().Table({{(PackageName "schema" $Model.Singular)}}.Table).Set(uc).Where(sqlbuilder.Eq({{(PackageName "schema" $Model.Singular)}}.ColumnID, sqlbuilder.Bind(m.ID)))

	qs, qv, err := sqlbuilder.NewSerializer(sqlbuilder.DialectPostgres{}).F(qb.AsStatement).ToSQL()
	if err != nil {
		return fmt.Errorf("{{$Model.Singular}}SQLSave: couldn't generate query: %w", err)
	}

	if _, err := db.ExecContext(ctx, qs, qv...); err != nil {
		return fmt.Errorf("{{$Model.Singular}}SQLSave: couldn't update record: %w", err)
	}

	return nil
}
{{end}}
`
