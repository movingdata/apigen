package apitypes

type Model struct {
	HTTPSearch     bool
	HTTPGet        bool
	HTTPCreate     bool
	HTTPUpdate     bool
	Singular       string
	Plural         string
	LowerPlural    string
	Fields         []Field
	SpecialOrders  []SpecialOrder
	SpecialFilters []Filter
	CanCreate      bool
	CanUpdate      bool
	CanHide        bool
	HasCreatedAt   bool
	HasUpdatedAt   bool
	HasCreatorID   bool
	HasUpdaterID   bool
	HasAudit       bool
	HasUserFilter  bool
}

type Field struct {
	GoName         string
	GoType         string
	IsNull         bool
	Array          bool
	APIName        string
	JSType         string
	JSONType       map[string]interface{}
	Filters        []Filter
	Masked         bool
	UserMask       string
	UserMaskValue  string
	IgnoreInput    bool
	Facet          bool
	NoOrder        bool
	OmitEmpty      bool
	Enum           Enums
	Sequence       string
	SequencePrefix string
}

type Enum struct {
	Value  string
	Label  string
	GoName string
}

type Enums []Enum

func (e Enums) First() Enum {
	return e[0]
}

type SpecialOrder struct {
	GoName  string
	APIName string
}

type Filter struct {
	Operator     string
	Name         string
	GoName       string
	GoType       string
	JSType       string
	JSONType     string
	TestOperator string
	TestType     string
}

type Table struct {
	Name   string
	Fields []TableField
}

func (t Table) Field(name string) *TableField {
	for _, e := range t.Fields {
		if e.Name == name {
			return &e
		}
	}

	return nil
}

func (t Table) String() string {
	s := "create table " + t.Name + "(\n"

	for i, e := range t.Fields {
		s += "  " + e.String()
		if i != len(t.Fields)-1 {
			s += ","
		}
		s += "\n"
	}

	s += ")\n"

	return s
}

type TableField struct {
	Name    string
	Type    string
	NotNull bool
	Array   bool
}

func (f TableField) String() string {
	s := f.Name + " " + f.Type

	if f.Array {
		s += "[]"
	}

	if f.NotNull {
		s += " not null"
	}

	return s
}
