package schemagen

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/terraform/helper/schema"
)

type testSchemaGeneratorCase struct {
	Name           string
	Subject        interface{}
	ExpectedSchema map[string]*schema.Schema
	ExpectedOut    string
	FilterFunc     FilterFunc
}

func testSchemaGeneratorFilterFuncPromoteEmbedded(gs *GenState) error {
	name := gs.CurrentField.Name
	typ := gs.CurrentField.Type.Name()

	if name == "testSchemaGeneratorNestedBase" && typ == "testSchemaGeneratorNestedBase" {
		gs.Action = ActionPromote
	}

	return nil
}

func testSchemaGeneratorFilterFuncModifyName(gs *GenState) error {
	if gs.CurrentName == "foo" {
		gs.CurrentName = "foo_bar"
	}

	return nil
}

func testSchemaGeneratorFilterFuncModifyAttributes(gs *GenState) error {
	if gs.CurrentName == "foo" {
		gs.CurrentSchema.Required = true
		gs.CurrentSchema.Description = "foobar"
		gs.CurrentSchema.Default = "barfoo"
	}

	return nil
}

func testSchemaGeneratorFilterFuncModifyInterface(gs *GenState) error {
	name := gs.CurrentField.Name

	if name == "Interface" {
		gs.CurrentField.Type = reflect.TypeOf(&testSchemaGeneratorNestedBase{})
	}

	return nil
}

type testSchemaGeneratorPrimitives struct {
	StringField string
	IntField    int
	BoolField   bool
	FloatField  float64
}

type testSchemaGeneratorNestedComplex struct {
	NestedComplex testSchemaGeneratorNestedBase
}

type testSchemaGeneratorSliceComplex struct {
	SliceComplex []testSchemaGeneratorNestedBase
}

type testSchemaGeneratorEmbedded struct {
	Embedded testSchemaGeneratorNestedEmbedded
}

type testSchemaGeneratorInterface struct {
	Interface interface{}
}

type testSchemaGeneratorNestedBase struct {
	Foo string
}

type testSchemaGeneratorNestedEmbedded struct {
	testSchemaGeneratorNestedBase

	ThingOne string
}

var testSchemaGeneratorCases = []testSchemaGeneratorCase{
	{
		Name:    "primitives",
		Subject: testSchemaGeneratorPrimitives{},
		ExpectedSchema: map[string]*schema.Schema{
			"bool_field": {
				Type: schema.TypeBool,
			},
			"float_field": {
				Type: schema.TypeFloat,
			},
			"int_field": {
				Type: schema.TypeInt,
			},
			"string_field": {
				Type: schema.TypeString,
			},
		},
		ExpectedOut: strings.TrimSpace(`
map[string]*schema.Schema{
	"bool_field": {
		Type: schema.TypeBool,
	},
	"float_field": {
		Type: schema.TypeFloat,
	},
	"int_field": {
		Type: schema.TypeInt,
	},
	"string_field": {
		Type: schema.TypeString,
	},
}
		`),
	},
	{
		Name:    "nested complex",
		Subject: testSchemaGeneratorNestedComplex{},
		ExpectedSchema: map[string]*schema.Schema{
			"nested_complex": {
				Type: schema.TypeList,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"foo": {
							Type: schema.TypeString,
						},
					},
				},
				MaxItems: 1,
			},
		},
		ExpectedOut: strings.TrimSpace(`
map[string]*schema.Schema{
	"nested_complex": {
		Type: schema.TypeList,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"foo": {
					Type: schema.TypeString,
				},
			},
		},
		MaxItems: 1,
	},
}
		`),
	},
	{
		Name:    "slice complex",
		Subject: testSchemaGeneratorSliceComplex{},
		ExpectedSchema: map[string]*schema.Schema{
			"slice_complex": {
				Type: schema.TypeSet,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"foo": {
							Type: schema.TypeString,
						},
					},
				},
			},
		},
		ExpectedOut: strings.TrimSpace(`
map[string]*schema.Schema{
	"slice_complex": {
		Type: schema.TypeSet,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"foo": {
					Type: schema.TypeString,
				},
			},
		},
	},
}
		`),
	},
	{
		Name:    "embedded, unfiltered",
		Subject: testSchemaGeneratorEmbedded{},
		ExpectedSchema: map[string]*schema.Schema{
			"embedded": {
				Type: schema.TypeList,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"test_schema_generator_nested_base": {
							Type: schema.TypeList,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"foo": {
										Type: schema.TypeString,
									},
								},
							},
							MaxItems: 1,
						},
						"thing_one": {
							Type: schema.TypeString,
						},
					},
				},
				MaxItems: 1,
			},
		},
		ExpectedOut: strings.TrimSpace(`
map[string]*schema.Schema{
	"embedded": {
		Type: schema.TypeList,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"test_schema_generator_nested_base": {
					Type: schema.TypeList,
					Elem: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"foo": {
								Type: schema.TypeString,
							},
						},
					},
					MaxItems: 1,
				},
				"thing_one": {
					Type: schema.TypeString,
				},
			},
		},
		MaxItems: 1,
	},
}
		`),
	},
	{
		Name:       "embedded, filtered and promoted",
		Subject:    testSchemaGeneratorEmbedded{},
		FilterFunc: testSchemaGeneratorFilterFuncPromoteEmbedded,
		ExpectedSchema: map[string]*schema.Schema{
			"embedded": {
				Type: schema.TypeList,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"foo": {
							Type: schema.TypeString,
						},
						"thing_one": {
							Type: schema.TypeString,
						},
					},
				},
				MaxItems: 1,
			},
		},
		ExpectedOut: strings.TrimSpace(`
map[string]*schema.Schema{
	"embedded": {
		Type: schema.TypeList,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"foo": {
					Type: schema.TypeString,
				},
				"thing_one": {
					Type: schema.TypeString,
				},
			},
		},
		MaxItems: 1,
	},
}
		`),
	},
	{
		Name:       "filtered, modified field name",
		Subject:    testSchemaGeneratorNestedBase{},
		FilterFunc: testSchemaGeneratorFilterFuncModifyName,
		ExpectedSchema: map[string]*schema.Schema{
			"foo_bar": {
				Type: schema.TypeString,
			},
		},
		ExpectedOut: strings.TrimSpace(`
map[string]*schema.Schema{
	"foo_bar": {
		Type: schema.TypeString,
	},
}
		`),
	},
	{
		Name:       "filtered, additional attributes",
		Subject:    testSchemaGeneratorNestedBase{},
		FilterFunc: testSchemaGeneratorFilterFuncModifyAttributes,
		ExpectedSchema: map[string]*schema.Schema{
			"foo": {
				Type:        schema.TypeString,
				Required:    true,
				Default:     "barfoo",
				Description: "foobar",
			},
		},
		ExpectedOut: strings.TrimSpace(`
map[string]*schema.Schema{
	"foo": {
		Type: schema.TypeString,
		Required: true,
		Default: "barfoo",
		Description: "foobar",
	},
}
		`),
	},
	{
		Name:           "interface, unfiltered",
		Subject:        testSchemaGeneratorInterface{},
		ExpectedSchema: map[string]*schema.Schema{},
		ExpectedOut: strings.TrimSpace(`
map[string]*schema.Schema{
}
		`),
	},
	{
		Name:       "interface, filtered and asserted to specific type",
		Subject:    testSchemaGeneratorInterface{},
		FilterFunc: testSchemaGeneratorFilterFuncModifyInterface,
		ExpectedSchema: map[string]*schema.Schema{
			"interface": {
				Type: schema.TypeList,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"foo": {
							Type: schema.TypeString,
						},
					},
				},
				MaxItems: 1,
			},
		},
		ExpectedOut: strings.TrimSpace(`
map[string]*schema.Schema{
	"interface": {
		Type: schema.TypeList,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"foo": {
					Type: schema.TypeString,
				},
			},
		},
		MaxItems: 1,
	},
}
		`),
	},
}

func TestSchemaGeneratorSchemaFromStruct(t *testing.T) {
	for _, tc := range testSchemaGeneratorCases {
		t.Run(tc.Name, func(t *testing.T) {
			sg := &SchemaGenerator{
				Obj:        tc.Subject,
				FilterFunc: tc.FilterFunc,
			}
			actual, err := sg.schemaFromStruct(sg.Obj)
			if err != nil {
				t.Fatalf("bad: %s", err)
			}
			if !reflect.DeepEqual(tc.ExpectedSchema, actual) {
				t.Fatalf("\nExpected:\n\n%s\n\nActual:\n\n%s", spew.Sdump(tc.ExpectedSchema), spew.Sdump(actual))
			}
		})
	}
}

func TestSchemaGeneratorSchemaFprint(t *testing.T) {
	for _, tc := range testSchemaGeneratorCases {
		t.Run(tc.Name, func(t *testing.T) {
			sg := &SchemaGenerator{
				Obj:        tc.Subject,
				FilterFunc: tc.FilterFunc,
			}
			s, err := sg.schemaFromStruct(sg.Obj)
			if err != nil {
				t.Fatalf("bad: %s", err)
			}
			var buf bytes.Buffer
			if err := sg.schemaFprint(&buf, s, 0); err != nil {
				t.Fatalf("bad: %s", err)
			}
			actual := strings.TrimSpace(buf.String())
			if tc.ExpectedOut != actual {
				t.Fatalf("\n===== Expected =====\n%s\n\n===== Actual =====\n%s\n", tc.ExpectedOut, actual)
			}
		})
	}
}
