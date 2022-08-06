package main

import (
	"encoding/json"
	"go/types"
	"io"
	"io/ioutil"
	"strings"
)

type SwaggerWriter struct {
	file        string
	definitions map[string]interface{}
	paths       map[string]map[string]interface{}
}

func NewSwaggerWriter(file string) *SwaggerWriter {
	return &SwaggerWriter{
		file:        file,
		definitions: make(map[string]interface{}),
		paths:       make(map[string]map[string]interface{}),
	}
}

func (SwaggerWriter) Name() string     { return "swagger" }
func (SwaggerWriter) Language() string { return "swagger" }
func (w SwaggerWriter) File(typeName string, namedType *types.Named, structType *types.Struct) string {
	return w.file
}

func (SwaggerWriter) Imports(typeName string, namedType *types.Named, structType *types.Struct) []string {
	return []string{}
}

func (w *SwaggerWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	model, err := makeModel(typeName, namedType, structType)
	if err != nil {
		return err
	}

	properties := map[string]interface{}{}

	for _, field := range model.Fields {
		properties[field.APIName] = field.JSONType
	}

	w.definitions[model.Singular] = map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}

	if model.HTTPSearch {
		var parameters []interface{}

		for _, field := range model.Fields {
			for _, filter := range field.Filters {
				parameter := map[string]interface{}{
					"in":       "query",
					"name":     filter.Name,
					"required": false,
				}

				switch filter.JSONType {
				case "boolean":
					parameter["type"] = "string"
					parameter["enum"] = []string{"true", "false"}
				default:
					parameter["type"] = filter.JSONType
				}

				if tpl, ok := swaggerFilterTemplates[filter.Operator]; ok {
					parameter["description"] = strings.Replace(tpl, "__NAME__", field.APIName, -1)
				}

				if len(field.Enum) > 0 {
					var values []string

					for _, e := range field.Enum {
						values = append(values, e.Value)
					}

					parameter["enum"] = values
				}

				parameters = append(parameters, parameter)
			}
		}

		for _, filter := range model.SpecialFilters {
			parameters = append(parameters, map[string]interface{}{
				"in":       "query",
				"name":     filter.Name,
				"required": false,
				"type":     filter.JSONType,
			})
		}

		var fieldNames []string
		for _, field := range model.Fields {
			if field.NoOrder {
				continue
			}

			fieldNames = append(fieldNames, field.APIName, "-"+field.APIName)
		}

		parameters = append(
			parameters,
			map[string]interface{}{
				"in":          "query",
				"name":        "order",
				"required":    false,
				"type":        "string",
				"enum":        fieldNames,
				"description": "Sort the result list by a specific field. Prepending a minus will sort in reverse.",
			},
			map[string]interface{}{
				"in":       "query",
				"name":     "page",
				"required": true,
				"type":     "string",
			},
		)

		if _, ok := w.paths["/"+model.LowerPlural]; !ok {
			w.paths["/"+model.LowerPlural] = map[string]interface{}{}
		}

		w.paths["/"+model.LowerPlural]["get"] = map[string]interface{}{
			"summary":    "Search for " + model.Singular + " records",
			"parameters": parameters,
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "The search was successful.",
					"schema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"records": map[string]interface{}{
								"type":  "array",
								"items": map[string]interface{}{"$ref": "#/definitions/" + model.Singular},
							},
							"total": map[string]interface{}{
								"type":        "number",
								"description": "The total number of results available for this search",
							},
							"time": map[string]interface{}{
								"type":        "string",
								"description": "The time these results were generated",
							},
						},
					},
				},
				"400": map[string]interface{}{
					"description": "Bad search parameters",
				},
				"401": map[string]interface{}{
					"description": "Unauthorised",
				},
				"500": map[string]interface{}{
					"description": "An unhandled or internal error occurred",
				},
			},
		}
	}

	if model.HTTPGet {
		if _, ok := w.paths["/"+model.LowerPlural+"/{id}"]; !ok {
			w.paths["/"+model.LowerPlural+"/{id}"] = map[string]interface{}{}
		}

		w.paths["/"+model.LowerPlural+"/{id}"]["get"] = map[string]interface{}{
			"summary": "Fetch a specific " + model.Singular + " record",
			"parameters": []interface{}{
				map[string]interface{}{
					"in":          "path",
					"name":        "id",
					"required":    true,
					"type":        "string",
					"description": "ID of the record to fetch",
				},
			},
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "The fetch was successful",
					"schema":      map[string]interface{}{"$ref": "#/definitions/" + model.Singular},
				},
				"404": map[string]interface{}{
					"description": "The specified record does not exist",
				},
				"401": map[string]interface{}{
					"description": "Unauthorised",
				},
				"500": map[string]interface{}{
					"description": "An unhandled or internal error occurred",
				},
			},
		}
	}

	if model.HTTPCreate {
		if _, ok := w.paths["/"+model.LowerPlural]; !ok {
			w.paths["/"+model.LowerPlural] = map[string]interface{}{}
		}

		w.paths["/"+model.LowerPlural]["post"] = map[string]interface{}{
			"summary": "Create a new " + model.Singular + " record",
			"parameters": []interface{}{
				map[string]interface{}{
					"in":          "body",
					"name":        model.Singular,
					"description": "The " + model.Singular + " record to create",
					"schema":      map[string]interface{}{"$ref": "#/definitions/" + model.Singular},
				},
			},
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "The record was created successfully",
					"schema":      map[string]interface{}{"$ref": "#/definitions/" + model.Singular},
				},
				"404": map[string]interface{}{
					"description": "The specified record does not exist",
				},
				"401": map[string]interface{}{
					"description": "Unauthorised",
				},
				"500": map[string]interface{}{
					"description": "An unhandled or internal error occurred",
				},
			},
		}
	}

	if model.HTTPUpdate {
		if _, ok := w.paths["/"+model.LowerPlural+"/{id}"]; !ok {
			w.paths["/"+model.LowerPlural+"/{id}"] = map[string]interface{}{}
		}

		w.paths["/"+model.LowerPlural+"/{id}"]["put"] = map[string]interface{}{
			"summary": "Create a new " + model.Singular + " record",
			"parameters": []interface{}{
				map[string]interface{}{
					"in":          "path",
					"name":        "id",
					"required":    true,
					"type":        "string",
					"description": "ID of the record to update",
				},
				map[string]interface{}{
					"in":          "body",
					"name":        model.Singular,
					"description": "The " + model.Singular + " record to update",
					"schema":      map[string]interface{}{"$ref": "#/definitions/" + model.Singular},
				},
			},
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "The record was updated successfully",
					"schema":      map[string]interface{}{"$ref": "#/definitions/" + model.Singular},
				},
				"404": map[string]interface{}{
					"description": "The specified record does not exist",
				},
				"401": map[string]interface{}{
					"description": "Unauthorised",
				},
				"500": map[string]interface{}{
					"description": "An unhandled or internal error occurred",
				},
			},
		}
	}

	return nil
}

func (w *SwaggerWriter) Finish(dry bool) error {
	v := map[string]interface{}{
		"basePath": "/api",
		"host":     "managedservicehub.tw",
		"info": map[string]interface{}{
			"description": "Interact with the Managed Service Hub",
			"title":       "Managed Service Hub API",
			"version":     "1.0.0",
		},
		"produces": []string{
			"application/json",
		},
		"schemes": []string{
			"https",
		},
		"swagger":     "2.0",
		"definitions": w.definitions,
		"paths":       w.paths,
	}

	d, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	if !dry {
		if err := ioutil.WriteFile(w.file, d, 0644); err != nil {
			return err
		}
	}

	return nil
}

var swaggerFilterTemplates = map[string]string{
	"=":                       "Find records where __NAME__ equals this value",
	"!=":                      "Find records where __NAME__ does not equal this value",
	"<":                       "Find records where __NAME__ is less than this value",
	"<=":                      "Find records where __NAME__ is less than or equal to this value",
	"is_null_or_less_than":    "Find records where __NAME__ is null or less than this value",
	">":                       "Find records where __NAME__ is greater than this value",
	">=":                      "Find records where __NAME__ is greater than or equal to this value",
	"is_null_or_greater_than": "Find records where __NAME__ is null or greater than this value",
	"in":                      "Find records where __NAME__ is one of the values specified, separated by commas",
	"not_in":                  "Find records where __NAME__ is not one of the values specified, separated by commas",
	"is_null":                 "Find records where __NAME__ is null",
	"is_not_null":             "Find records where __NAME__ is not null",
	"@@":                      "Find records where __NAME__ contains this term or phrase in a full text search",
	"contains":                "Find records where __NAME__ contains this value",
	"prefix":                  "Find records where __NAME__ starts with this value",
	"@>":                      "Find records where __NAME__ is a superset of this value",
	"!@>":                     "Find records where __NAME__ is not a superset of this value",
	"<@":                      "Find records where __NAME__ is a subset of this value",
	"!<@":                     "Find records where __NAME__ is not a subset of this value",
	"&&":                      "Find records where __NAME__ intersects with this value",
	"!&&":                     "Find records where __NAME__ does not intersect with this value",
}
