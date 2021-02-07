package main

import (
	"go/types"
	"io"
	"strings"
	"text/template"
)

type OttoWriter struct{ Dir string }

func NewOttoWriter(dir string) *OttoWriter { return &OttoWriter{Dir: dir} }

func (OttoWriter) Name() string     { return "otto" }
func (OttoWriter) Language() string { return "go" }
func (w OttoWriter) File(typeName string, _ *types.Named, _ *types.Struct) string {
	return w.Dir + "/" + strings.ToLower(typeName) + "_otto.go"
}

func (OttoWriter) Imports() []string {
	return []string{
    "time",
    "github.com/robertkrimen/otto",
		"github.com/satori/go.uuid",
	}
}

func (w *OttoWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	model, err := makeModel(typeName, namedType, structType)
	if err != nil {
		return err
	}
	return ottoTemplate.Execute(wr, *model)
}

var ottoTemplate = template.Must(template.New("ottoTemplate").Funcs(tplFunc).Parse(`
{{$Type := .}}

func (jsctx *JSContext) {{$Type.Singular}}Get(id uuid.UUID) *{{$Type.Singular}} {
  v, err := jsctx.mctx.{{$Type.Singular}}APIGet(jsctx.ctx, jsctx.tx, id, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (jsctx *JSContext) {{$Type.Singular}}Search(p {{$Type.Singular}}APISearchParameters) *{{$Type.Singular}}APISearchResponse {
  v, err := jsctx.mctx.{{$Type.Singular}}APISearch(jsctx.ctx, jsctx.tx, &p, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (jsctx *JSContext) {{$Type.Singular}}Find(p {{$Type.Singular}}APIFilterParameters) *{{$Type.Singular}} {
  v, err := jsctx.mctx.{{$Type.Singular}}APIFind(jsctx.ctx, jsctx.tx, &p, &jsctx.uid, &jsctx.euid)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

{{if $Type.CanCreate}}
func (jsctx *JSContext) {{$Type.Singular}}Create(input {{$Type.Singular}}) *{{$Type.Singular}} {
  v, err := jsctx.mctx.{{$Type.Singular}}APICreate(jsctx.ctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), &input, nil)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (jsctx *JSContext) {{$Type.Singular}}CreateWithOptions(input {{$Type.Singular}}, options APIOptions) *{{$Type.Singular}} {
  v, err := jsctx.mctx.{{$Type.Singular}}APICreate(jsctx.ctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), &input, &options)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}
{{end}}

{{if $Type.CanUpdate}}
func (jsctx *JSContext) {{$Type.Singular}}Save(input *{{$Type.Singular}}) *{{$Type.Singular}} {
  v, err := jsctx.mctx.{{$Type.Singular}}APISave(jsctx.ctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), input, nil)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}

func (jsctx *JSContext) {{$Type.Singular}}SaveWithOptions(input *{{$Type.Singular}}, options *APIOptions) *{{$Type.Singular}} {
  v, err := jsctx.mctx.{{$Type.Singular}}APISave(jsctx.ctx, jsctx.tx, jsctx.uid, jsctx.euid, time.Now(), input, options)
  if err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
  return v
}
{{end}}

{{if $Type.HasCreatedAt}}
func (jsctx *JSContext) {{$Type.Singular}}ChangeCreatedAt(id uuid.UUID, createdAt time.Time) {
  if err := jsctx.mctx.{{$Type.Singular}}APIChangeCreatedAt(jsctx.ctx, jsctx.tx, id, createdAt); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}
{{end}}

{{if $Type.HasCreatorID}}
func (jsctx *JSContext) {{$Type.Singular}}ChangeCreatorID(id, creatorID uuid.UUID) {
  if err := jsctx.mctx.{{$Type.Singular}}APIChangeCreatorID(jsctx.ctx, jsctx.tx, id, creatorID); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}
{{end}}

{{if $Type.HasUpdatedAt}}
func (jsctx *JSContext) {{$Type.Singular}}ChangeUpdatedAt(id uuid.UUID, updatedAt time.Time) {
  if err := jsctx.mctx.{{$Type.Singular}}APIChangeUpdatedAt(jsctx.ctx, jsctx.tx, id, updatedAt); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}
{{end}}

{{if $Type.HasUpdaterID}}
func (jsctx *JSContext) {{$Type.Singular}}ChangeUpdaterID(id, updaterID uuid.UUID) {
  if err := jsctx.mctx.{{$Type.Singular}}APIChangeUpdaterID(jsctx.ctx, jsctx.tx, id, updaterID); err != nil {
    panic(jsctx.vm.MakeCustomError("InternalError", err.Error()))
  }
}
{{end}}
`))
