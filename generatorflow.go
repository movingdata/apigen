package main

import (
  "encoding/json"
  "io"
  "strings"
)

type FlowGenerator struct {
  dir string
}

func NewFlowGenerator(dir string) *FlowGenerator {
  return &FlowGenerator{dir: dir}
}

func (g *FlowGenerator) Name() string {
  return "flow"
}

func (g *FlowGenerator) Model(model *Model) []writer {
  return []writer{
    &basicWriter{
      name:     "individual",
      language: "flow",
      file:     g.dir + "/global_db_model_" + strings.ToLower(model.Singular) + ".js",
      write:    templateWriter(flowTemplate, map[string]interface{}{"Model": model}),
    },
  }
}

func (g *FlowGenerator) Models(models []*Model) []writer {
  return []writer{
    &basicWriter{
      name:     "aggregated/flow",
      language: "flow",
      file:     g.dir + "/global_db.js",
      write:    templateWriter(flowFinishTemplate, map[string]interface{}{"Models": models}),
    },
    &basicWriter{
      name:     "aggregated/manifest",
      language: "flow",
      file:     g.dir + "/global_db_manifest.json",
      write: func(wr io.Writer) error {
        var files []string

        for _, e := range models {
          files = append(files, "global_db_model_"+strings.ToLower(e.Singular)+".js")
        }

        return json.NewEncoder(wr).Encode(struct {
          Files []string `json:"files"`
        }{files})
      },
    },
  }
}

var flowTemplate = `
{{$Model := .Model}}

{{range $Field := $Model.Fields}}
{{- if $Field.Enum}}
type global_db_{{$Model.Singular}}{{$Field.GoName}} =
{{- range $Enum := $Field.Enum}}
  | '{{$Enum.Value}}'
{{- end}}
{{- end}}
{{end}}

type global_db_{{$Model.Singular}} = {|
{{- range $Field := $Model.Fields}}
  {{$Field.APIName}}: {{$Field.FlowType}},
{{- end}}
|};

type global_db_{{$Model.Singular}}_FilterParameters = {|
{{- range $Field := $Model.Fields}}
{{- range $Filter := $Field.Filters}}
  {{$Filter.Name}}?: {{$Filter.FlowType}},
{{- end}}
{{- end}}
{{- range $Filter := $Model.SpecialFilters}}
  {{$Filter.Name}}?: {{$Filter.FlowType}},
{{- end}}
|};

type global_db_{{$Model.Singular}}_SearchParameters = {|
  ...global_db_{{$Model.Singular}}_FilterParameters,
  order?: string,
  offset?: number,
  limit?: number,
|};

type global_db_{{$Model.Singular}}_SearchResponse = {|
  records: $ReadOnlyArray<global_db_{{$Model.Singular}}>,
  total: number,
  time: global_time_Time,
|};
`

var flowFinishTemplate = `
{{$Models := .Models}}

type global_db_CallbackSpecifier = {|
  model?: string,
  name?: string,
  ids?: $ReadOnlyArray<global_uuid_UUID>,
  once?: bool,
  used?: bool,
|};

type global_db_APIOptions = {|
  skipCallbacks: $ReadOnlyArray<global_db_CallbackSpecifier>,
  forceCallbacks: $ReadOnlyArray<global_db_CallbackSpecifier>,
|};

declare class global_db_DB {
{{- range $Model := $Models}}
  {{$Model.Singular}}Get(id: {{$Model.IDField.FlowType}}): ?global_db_{{$Model.Singular}};
  {{$Model.Singular}}Search(p: global_db_{{$Model.Singular}}_SearchParameters): global_db_{{$Model.Singular}}_SearchResponse;
  {{$Model.Singular}}Find(p: global_db_{{$Model.Singular}}_FilterParameters): ?global_db_{{$Model.Singular}};
{{- if $Model.HasAPICreate}}
  {{$Model.Singular}}Create(input: global_db_{{$Model.Singular}}): global_db_{{$Model.Singular}};
  {{$Model.Singular}}CreateWithOptions(input: global_db_{{$Model.Singular}}, options: global_db_APIOptions): global_db_{{$Model.Singular}};
{{- end}}
{{- if $Model.HasAPIUpdate}}
  {{$Model.Singular}}Save(input: global_db_{{$Model.Singular}}): global_db_{{$Model.Singular}};
  {{$Model.Singular}}SaveWithOptions(input: global_db_{{$Model.Singular}}, options: global_db_APIOptions): global_db_{{$Model.Singular}};
{{- end}}
{{- if $Model.HasCreatedAt}}
  {{$Model.Singular}}ChangeCreatedAt(id: {{$Model.IDField.FlowType}}, createdAt: global_time_Time): void;
{{- end}}
{{- if $Model.HasCreatorID}}
  {{$Model.Singular}}ChangeCreatorID(id: {{$Model.IDField.FlowType}}, creatorId: global_uuid_UUID): void;
{{- end}}
{{- if $Model.HasUpdatedAt}}
  {{$Model.Singular}}ChangeUpdatedAt(id: {{$Model.IDField.FlowType}}, updatedAt: global_time_Time): void;
{{- end}}
{{- if $Model.HasUpdaterID}}
  {{$Model.Singular}}ChangeUpdaterID(id: {{$Model.IDField.FlowType}}, updaterId: global_uuid_UUID): void;
{{- end}}
{{- end}}
};

declare var db: global_db_DB;

declare function dbAs(userId: global_uuid_UUID, effectiveUserId: global_uuid_UUID): global_db_DB;
`
