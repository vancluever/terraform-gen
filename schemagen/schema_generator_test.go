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
	Name              string
	Subject           interface{}
	ExpectedSchema    map[string]*schema.Schema
	ExpectedSchemaOut string
	// Note that ExpectedExpandOut contains package paths in type names even
	// though said types are local to this package. This is a known issue and may
	// not be resolveable in a sane way, and more than likely is a non-issue in
	// production as most types will be included from a separate package.
	ExpectedExpandOut string
	FilterFunc        FilterFunc
}

func testSchemaGeneratorFilterFuncPromoteEmbedded(fs *GenFieldState) error {
	name := fs.CurrentField.Name
	typ := fs.CurrentField.Type.Name()

	if name == "testSchemaGeneratorNestedBase" && typ == "testSchemaGeneratorNestedBase" {
		fs.Action = ActionPromote
	}

	return nil
}

func testSchemaGeneratorFilterFuncModifyName(fs *GenFieldState) error {
	if fs.CurrentName == "foo" {
		fs.CurrentName = "foo_bar"
	}

	return nil
}

func testSchemaGeneratorFilterFuncModifyAttributes(fs *GenFieldState) error {
	if fs.CurrentName == "foo" {
		fs.CurrentSchema.Required = true
		fs.CurrentSchema.Description = "foobar"
		fs.CurrentSchema.Default = "barfoo"
	}

	return nil
}

func testSchemaGeneratorFilterFuncModifyInterface(fs *GenFieldState) error {
	name := fs.CurrentField.Name

	if name == "Interface" {
		fs.CurrentField.Type = reflect.TypeOf(&testSchemaGeneratorNestedBase{})
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
		ExpectedSchemaOut: strings.TrimSpace(`
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
		ExpectedExpandOut: strings.TrimSpace(`
func expandTestSchemaGeneratorPrimitives(d *schema.ResourceData) schemagen.testSchemaGeneratorPrimitives {
	obj := schemagen.testSchemaGeneratorPrimitives{}
	obj.BoolField = d.Get("bool_field").(bool)
	obj.FloatField = d.Get("float_field").(float64)
	obj.IntField = d.Get("int_field").(int)
	obj.StringField = d.Get("string_field").(string)
	return obj
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
		ExpectedSchemaOut: strings.TrimSpace(`
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
		ExpectedExpandOut: strings.TrimSpace(`
func expandTestSchemaGeneratorNestedComplex(d *schema.ResourceData) schemagen.testSchemaGeneratorNestedComplex {
	obj := schemagen.testSchemaGeneratorNestedComplex{}
	mNestedComplex := d.Get("nested_complex").([]interface{})[0].(map[string]interface{})
	obj.NestedComplex = expandTestSchemaGeneratorNestedBase(mNestedComplex)
	return obj
}

func expandTestSchemaGeneratorNestedBase(d map[string]interface{}) schemagen.testSchemaGeneratorNestedBase {
	obj := schemagen.testSchemaGeneratorNestedBase{}
	obj.Foo = d["foo"].(string)
	return obj
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
		ExpectedSchemaOut: strings.TrimSpace(`
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
		ExpectedExpandOut: strings.TrimSpace(`
func expandTestSchemaGeneratorSliceComplex(d *schema.ResourceData) schemagen.testSchemaGeneratorSliceComplex {
	obj := schemagen.testSchemaGeneratorSliceComplex{}
	var sSliceComplex []schemagen.testSchemaGeneratorNestedBase
	uSliceComplex := d.Get("slice_complex").([]interface{})
	for _, v := range uSliceComplex {
		w = expandTestSchemaGeneratorNestedBase(v.(map[string]interface{}))
		sSliceComplex = append(sSliceComplex, w)
	}
	obj.SliceComplex = sSliceComplex
	return obj
}

func expandTestSchemaGeneratorNestedBase(d map[string]interface{}) schemagen.testSchemaGeneratorNestedBase {
	obj := schemagen.testSchemaGeneratorNestedBase{}
	obj.Foo = d["foo"].(string)
	return obj
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
		ExpectedSchemaOut: strings.TrimSpace(`
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
		ExpectedExpandOut: strings.TrimSpace(`
func expandTestSchemaGeneratorEmbedded(d *schema.ResourceData) schemagen.testSchemaGeneratorEmbedded {
	obj := schemagen.testSchemaGeneratorEmbedded{}
	mEmbedded := d.Get("embedded").([]interface{})[0].(map[string]interface{})
	obj.Embedded = expandTestSchemaGeneratorNestedEmbedded(mEmbedded)
	return obj
}

func expandTestSchemaGeneratorNestedEmbedded(d map[string]interface{}) schemagen.testSchemaGeneratorNestedEmbedded {
	obj := schemagen.testSchemaGeneratorNestedEmbedded{}
	mtestSchemaGeneratorNestedBase := d["test_schema_generator_nested_base"].([]interface{})[0].(map[string]interface{})
	obj.testSchemaGeneratorNestedBase = expandTestSchemaGeneratorNestedBase(mtestSchemaGeneratorNestedBase)
	obj.ThingOne = d["thing_one"].(string)
	return obj
}

func expandTestSchemaGeneratorNestedBase(d map[string]interface{}) schemagen.testSchemaGeneratorNestedBase {
	obj := schemagen.testSchemaGeneratorNestedBase{}
	obj.Foo = d["foo"].(string)
	return obj
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
		ExpectedSchemaOut: strings.TrimSpace(`
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
		ExpectedExpandOut: strings.TrimSpace(`
func expandTestSchemaGeneratorEmbedded(d *schema.ResourceData) schemagen.testSchemaGeneratorEmbedded {
	obj := schemagen.testSchemaGeneratorEmbedded{}
	mEmbedded := d.Get("embedded").([]interface{})[0].(map[string]interface{})
	obj.Embedded = expandTestSchemaGeneratorNestedEmbedded(mEmbedded)
	return obj
}

func expandTestSchemaGeneratorNestedEmbedded(d map[string]interface{}) schemagen.testSchemaGeneratorNestedEmbedded {
	obj := schemagen.testSchemaGeneratorNestedEmbedded{}
	obj.Foo = d["foo"].(string)
	obj.ThingOne = d["thing_one"].(string)
	return obj
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
		ExpectedSchemaOut: strings.TrimSpace(`
map[string]*schema.Schema{
	"foo_bar": {
		Type: schema.TypeString,
	},
}
		`),
		ExpectedExpandOut: strings.TrimSpace(`
func expandTestSchemaGeneratorNestedBase(d *schema.ResourceData) schemagen.testSchemaGeneratorNestedBase {
	obj := schemagen.testSchemaGeneratorNestedBase{}
	obj.Foo = d.Get("foo_bar").(string)
	return obj
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
		ExpectedSchemaOut: strings.TrimSpace(`
map[string]*schema.Schema{
	"foo": {
		Type: schema.TypeString,
		Required: true,
		Default: "barfoo",
		Description: "foobar",
	},
}
		`),
		ExpectedExpandOut: strings.TrimSpace(`
func expandTestSchemaGeneratorNestedBase(d *schema.ResourceData) schemagen.testSchemaGeneratorNestedBase {
	obj := schemagen.testSchemaGeneratorNestedBase{}
	obj.Foo = d.Get("foo").(string)
	return obj
}
		`),
	},
	{
		Name:           "interface, unfiltered",
		Subject:        testSchemaGeneratorInterface{},
		ExpectedSchema: map[string]*schema.Schema{},
		ExpectedSchemaOut: strings.TrimSpace(`
map[string]*schema.Schema{
}
		`),
		ExpectedExpandOut: strings.TrimSpace(`
func expandTestSchemaGeneratorInterface(d *schema.ResourceData) schemagen.testSchemaGeneratorInterface {
	obj := schemagen.testSchemaGeneratorInterface{}
	return obj
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
		ExpectedSchemaOut: strings.TrimSpace(`
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
		ExpectedExpandOut: strings.TrimSpace(`
func expandTestSchemaGeneratorInterface(d *schema.ResourceData) schemagen.testSchemaGeneratorInterface {
	obj := schemagen.testSchemaGeneratorInterface{}
	mInterface := d.Get("interface").([]interface{})[0].(map[string]interface{})
	obj.Interface = expandTestSchemaGeneratorNestedBase(mInterface)
	return obj
}

func expandTestSchemaGeneratorNestedBase(d map[string]interface{}) schemagen.testSchemaGeneratorNestedBase {
	obj := schemagen.testSchemaGeneratorNestedBase{}
	obj.Foo = d["foo"].(string)
	return obj
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
			actual, _, err := sg.schemaFromStruct(sg.Obj)
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
			s, _, err := sg.schemaFromStruct(sg.Obj)
			if err != nil {
				t.Fatalf("bad: %s", err)
			}
			var buf bytes.Buffer
			if err := sg.schemaFprint(&buf, s, nil); err != nil {
				t.Fatalf("bad: %s", err)
			}
			actual := strings.TrimSpace(buf.String())
			if tc.ExpectedSchemaOut != actual {
				t.Fatalf("\n===== Expected =====\n%s\n\n===== Actual =====\n%s\n", tc.ExpectedSchemaOut, actual)
			}
		})
	}
}
func TestSchemaGeneratorExpandFprint(t *testing.T) {
	for _, tc := range testSchemaGeneratorCases {
		t.Run(tc.Name, func(t *testing.T) {
			sg := &SchemaGenerator{
				Obj:        tc.Subject,
				FilterFunc: tc.FilterFunc,
			}
			_, m, err := sg.schemaFromStruct(sg.Obj)
			if err != nil {
				t.Fatalf("bad: %s", err)
			}
			bufSlice, err := sg.expandFprint(m, nil)
			if err != nil {
				t.Fatalf("bad: %s", err)
			}
			var funcSlice []string
			for _, buf := range bufSlice {
				funcSlice = append(funcSlice, buf.String())
			}
			actual := strings.TrimSpace(strings.Join(funcSlice, "\n\n"))
			if tc.ExpectedExpandOut != actual {
				t.Fatalf("\n===== Expected =====\n%s\n\n===== Actual =====\n%s\n", tc.ExpectedExpandOut, actual)
			}
		})
	}
}
