package schemagen

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
	u "github.com/radeksimko/terraform-gen/internal/util"
)

// Action is a specific action for the generator to undertake.
type Action int

const (
	// ActionDefault denotes the default generator behaviour for a field.
	ActionDefault Action = iota

	// ActionSkip tells the generator to skip the field currently being processed. It
	// will not be added to the final output.
	ActionSkip

	// ActionPromote tells the generator to promote all subfields of this field
	// to the current schema level. This only applies to fields of struct type
	// and is ignored otherwise.
	ActionPromote
)

// GenFieldState represents the state of an in-progress schema generation
// process for a single field.
type GenFieldState struct {
	// The current parsed field name being worked on. This will be the final
	// element name in the schema for this field, unless manipulated in a filter.
	CurrentName string

	// The current struct field being worked on.
	CurrentField *reflect.StructField

	// The current schema being worked on. This can be updated by a filter
	// in-place to alter the final schema that gets output.
	CurrentSchema *schema.Schema

	// The schema action to undertake for this field.
	Action Action
}

// genState represents the state and raw data from a (usually completed)
// generation operation.
type genState struct {
	// The underlying schema.
	Schema map[string]*schema.Schema

	// Metadata for the Schema. The underlying map in the Schema field under this
	// type is hierarchially symmetrical with the actual schema.
	Meta *genMetaResource
}

// genMetaResource contains metadata on a specific resource field in the
// schema hierarchy - be it a top-level resource or a nested or list element.
// It's mainly used for storing type information that is not related to the
// actual schema, but otherwise is symmetrical with the schema being generated.
type genMetaResource struct {
	// The master type for this resource.
	Type reflect.Type

	// The underlying schema metadata.
	Schema map[string]*genMetaSchema
}

// genMetaSchema is the schema counterpart to SchemaGenMetaResource,
// designed to represent and store metadata for schema fields versus resource
// fields.
type genMetaSchema struct {
	// The field information for this schema item.
	Field reflect.StructField

	// A sub-resource, if any.
	Elem *genMetaResource
}

// FilterFunc is an optional filter function that can be used to transform a
// passed in GenFieldState, allowing the ability to alter field naming, type
// processing, or the generated schema. See GenFieldState for more information
// on the behaviour of manipulating certain fields.
type FilterFunc func(*GenFieldState) error

// SchemaGenerator is a printer.Generator for generating schema source files.
type SchemaGenerator struct {
	// The subject object to generate for.
	Obj interface{}

	// The filename within the target package to write the output to.
	File string

	// The variable within in the package that will encompass the schema.
	// Remember to make sure this does not collide with other variables being
	// generated or else you will run into generation errors.
	VariableName string

	// If defined, FilterFunc will be called to transform the state that is
	// passed into it - by either manipulating the field name, type, or schema
	// passed into it, and returning a new transformed state, a FilterFunc can
	// help control the final schema output, and is essential for use in more
	// complex generation scenarios.
	FilterFunc FilterFunc
}

// Filename implements printer.Generator for SchemaGenerator.
func (g *SchemaGenerator) Filename() string {
	return g.File
}

// Run implements printer.Generator for SchemaGenerator.
func (g *SchemaGenerator) Run(w io.Writer) error {
	state := new(genState)
	var err error
	state.Schema, state.Meta, err = g.schemaFromStruct(g.Obj)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, "import (\n\t\"github.com/hashicorp/terraform/helper/schema\")\n\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "var %s = ", g.VariableName); err != nil {
		return err
	}
	return g.schemaFprint(w, state.Schema, nil)
}

// schemaFromStruct generates a map[string]*schema.Schema from the supplied
// struct.
func (g *SchemaGenerator) schemaFromStruct(subj interface{}) (map[string]*schema.Schema, *genMetaResource, error) {
	fields := make(map[string]*schema.Schema)
	meta := &genMetaResource{
		Type:   reflect.TypeOf(subj),
		Schema: make(map[string]*genMetaSchema),
	}
	fs := new(GenFieldState)

	rawType := u.DereferencePtrType(reflect.TypeOf(subj))
	for i := 0; i < rawType.NumField(); i++ {
		metaSchema := new(genMetaSchema)

		f := rawType.Field(i)
		fs.CurrentField = &f
		fs.CurrentName = u.Underscore(fs.CurrentField.Name)
		fs.CurrentSchema = &schema.Schema{}
		if g.FilterFunc != nil {
			if err := g.FilterFunc(fs); err != nil {
				return fields, meta, err
			}
		}

		if fs.Action == ActionSkip {
			continue
		}
		metaSchema.Field = *fs.CurrentField
		t := u.DereferencePtrType(fs.CurrentField.Type)
		k := t.Kind()
		if fs.CurrentSchema.Type == schema.TypeInvalid {
			fs.CurrentSchema.Type = typeFor(k, t)
			if fs.CurrentSchema.Type == schema.TypeInvalid {
				// Still can't determine the field that we are looking for, skip this
				// one. More than likely it's an interface type that requires further
				// filtering.
				continue
			}
		}
		// We need to perform additional actions for non-primitive types
		switch fs.CurrentSchema.Type {
		case schema.TypeList:
			fallthrough
		case schema.TypeSet:
			switch k {
			case reflect.Slice:
				et := t.Elem().Kind()
				if et != reflect.Struct {
					fs.CurrentSchema.Elem = &schema.Schema{Type: typeFor(t.Elem().Kind(), t.Elem())}
					break
				}
				// If we got this far, this is a set of complex resources. Logic is a
				// simpler subset of the complex nested resource logic below, so we
				// can't fallthrough.
				v := reflect.New(t.Elem()).Elem().Interface()
				e, m, err := g.schemaFromStruct(v)
				if err != nil {
					return fields, meta, err
				}
				fs.CurrentSchema.Elem = &schema.Resource{Schema: e}
				metaSchema.Elem = m
			case reflect.Struct:
				// This is a complex resource! Element assignment is basically the
				// product of recursion from here.
				v := reflect.New(t).Elem().Interface()
				e, m, err := g.schemaFromStruct(v)
				if err != nil {
					return fields, meta, err
				}
				// If we are promoting then we are actually merging the fields that we
				// got from this iteration into our current field set. This allows
				// things like embedded fields to promote themselves when they would
				// otherwise be unnecessarily nested.
				if fs.Action == ActionPromote {
					for k, v := range e {
						_, fok := fields[k]
						_, mok := meta.Schema[k]
						if fok || mok {
							// Don't necessarily differentiate here, as the two fields should
							// always be in sync.
							return fields, meta, fmt.Errorf("key %q conflicts with key already in the schema. Data: %#v", k, v)
						}
						fields[k] = v
						meta.Schema[k] = m.Schema[k]
					}
					// Move on to the next field from here to avoid setting the schema twice
					continue
				}
				fs.CurrentSchema.Elem = &schema.Resource{Schema: e}
				fs.CurrentSchema.MaxItems = 1
				metaSchema.Elem = m
			}
		}
		if fs.CurrentName != "" {
			fields[fs.CurrentName] = fs.CurrentSchema
			meta.Schema[fs.CurrentName] = metaSchema
		}
	}
	// We are done - return the schema fields.
	return fields, meta, nil
}

// typeFor is a helper that converts a reflect.Kind into an appropriate
// schema.ValueType.
//
// Note that this takes some hard lines on what a slice and set are. Filters
// may need to be applied in situations where a slice does not necessarily mean
// a set (ie: it represents elements that need not be unique or where order
// matters), or if for some reason a complex sub-resource needs to be hashed.
func typeFor(k reflect.Kind, t reflect.Type) schema.ValueType {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return schema.TypeInt
	case reflect.Float32, reflect.Float64:
		return schema.TypeFloat
	case reflect.String:
		return schema.TypeString
	case reflect.Bool:
		return schema.TypeBool
	case reflect.Slice:
		// A slice of primitives is more than likely to have order matter than a
		// slice of complex values, which is more than likely to be unique.
		// Exceptions this rule is something for a filter to decide.
		if typeFor(t.Elem().Kind(), t.Elem()) != schema.TypeList {
			return schema.TypeList
		}
		return schema.TypeSet
	case reflect.Map:
		return schema.TypeMap
	case reflect.Struct:
		return schema.TypeList
	}
	return schema.TypeInvalid
}

// schemaFprint dumps the passed in map[string]*schema.Schema to the passed in
// io.Writer.
//
// Some level of pretty-printing (with tabbing level dictated by tc) is done
// just to make sure the code is syntatically correct. It should still be
// passed through format.
func (g *SchemaGenerator) schemaFprint(w io.Writer, s map[string]*schema.Schema, p *u.TabPrinter) error {
	if p == nil {
		p = u.NewTabPrinter(0)
	}
	p.Fprintf(w, "map[string]*schema.Schema{\n")
	// We need to sort our keys here because map iteration is non-deterministic,
	// making this impossible to test by default.
	var keys []string
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := s[k]
		p.Fprintf(w, "%q: {\n", k)
		srt := u.DereferencePtrType(reflect.TypeOf(v))
		for i := 0; i < srt.NumField(); i++ {
			rf := srt.Field(i)
			rt := rf.Type
			rn := rf.Name
			rv := reflect.ValueOf(*v).Field(i)
			ri := rv.Interface()
			rk := rv.Kind()
			switch rk {
			case reflect.Slice:
				// We don't print empty slice attributes
				if rv.Len() < 1 {
					continue
				}
			case reflect.Func:
				// We currently don't support function fields
				continue
			default:
				if reflect.Zero(rt).Interface() == ri {
					// We don't print zero values
					continue
				}
			}
			switch rn {
			// If you don't see a field here, it's not supported for printing right
			// now. Note that this includes function fields for now, so if you need
			// validation functions and what not, they will need to be added by hand
			// later.
			case "Type":
				p.Fprintf(w, "Type: schema.%s,\n", ri)
			case "Optional":
				p.Fprintf(w, "Optional: %t,\n", ri)
			case "Required":
				p.Fprintf(w, "Required: %t,\n", ri)
			case "Computed":
				p.Fprintf(w, "Computed: %t,\n", ri)
			case "ForceNew":
				p.Fprintf(w, "ForceNew: %t,\n", ri)
			case "Sensitive":
				p.Fprintf(w, "Sensitive: %t,\n", ri)
			case "Description":
				p.Fprintf(w, "Description: %q,\n", ri)
			case "Deprecated":
				p.Fprintf(w, "Deprecated: %q,\n", ri)
			case "Removed":
				p.Fprintf(w, "Removed: %q,\n", ri)
			case "MinItems":
				p.Fprintf(w, "MinItems: %d,\n", ri)
			case "MaxItems":
				p.Fprintf(w, "MaxItems: %d,\n", ri)
			case "Default":
				p.Fprintf(w, "Default: %s,\n", valToStr(ri))
			case "InputDefault":
				p.Fprintf(w, "InputDefault: %s,\n", valToStr(ri))
			case "Elem":
				switch t := ri.(type) {
				case *schema.Schema:
					p.Fprintf(w, "Elem: &schema.Schema{Type: schema.%s},\n", t.Type)
				case *schema.Resource:
					p.Fprintf(w, "Elem: &schema.Resource{\n")
					p.Fprintf(w, "Schema: ")
					g.schemaFprint(w, t.Schema, p)
					p.Fprintf(w, "},\n")
				}
			}
		}
		p.Fprintf(w, "},\n")
	}
	p.Fprintf(w, "}")
	if p.Count() > 0 {
		p.Fprintf(w, ",")
	}
	p.Fprintf(w, "\n")

	return nil
}

// valToStr returns a non-zero string for an interface value. It panics if it
// can't find the type, or if the result is zero.
func valToStr(v interface{}) string {
	var s string
	switch w := v.(type) {
	case int:
		s = strconv.Itoa(w)
	case string:
		s = fmt.Sprintf("%q", w)
	case bool:
		s = strconv.FormatBool(w)
	default:
		panic(fmt.Errorf("Unhandled type %T", v))
	}
	if s == "" {
		panic("string parsed to empty string")
	}
	return s
}

type expandFlattenFprintType string

const (
	expandFlattenFprintTypeResourceData = expandFlattenFprintType("*schema.ResourceData")
	expandFlattenFprintTypeMap          = expandFlattenFprintType("map[string]interface{}")
)

func (t expandFlattenFprintType) expandPrint(name string) string {
	var f string
	switch t {
	case expandFlattenFprintTypeResourceData:
		f = "d.Get(%q)"
	case expandFlattenFprintTypeMap:
		f = "d[%q]"
	}
	return fmt.Sprintf(f, name)
}

// expandFprint prints expanders for a supplied genState.
//
// This function is recursive - it returns a slice of *bytes.Buffer. Each
// buffer in the slice represents the output of a single function within the
// chain. The caller is expected to sequentially output the contents of each
// buffer into a target io.Writer.
func (g *SchemaGenerator) expandFprint(meta *genMetaResource, bufSlice []*bytes.Buffer) ([]*bytes.Buffer, error) {
	p := u.NewTabPrinter(0)
	var buf bytes.Buffer
	bufSlice = append(bufSlice, &buf)
	// Determine what we are dealing with here. The 1st layer is always a
	// *schema.ResourceData, but all sub-layers (complex resources) will be
	// map[string]interface{}.
	var eft expandFlattenFprintType
	if len(bufSlice) > 1 {
		// Sub-resource
		eft = expandFlattenFprintTypeMap
	} else {
		// Top level
		eft = expandFlattenFprintTypeResourceData
	}

	rt := meta.Type
	rs := rt.String()
	rn := rt.Name()
	p.Fprintf(&buf, "func expand%s(d %s) %s {\n", strings.Title(rn), eft, rs)

	// Declaration literal
	var rsp string
	rsb := strings.TrimPrefix(rs, "*")
	if rs != rsb {
		rsp = "&"
	}
	p.Fprintf(&buf, "obj := %s%s{}\n", rsp, rsb)

	// We need to sort our keys here because map iteration is non-deterministic,
	// making this impossible to test by default.
	var schemaKeys []string
	for k := range meta.Schema {
		schemaKeys = append(schemaKeys, k)
	}
	sort.Strings(schemaKeys)
	for _, sk := range schemaKeys {
		sm := meta.Schema[sk]
		fn := sm.Field.Name
		ft := u.DereferencePtrType(sm.Field.Type)
		fk := ft.Kind()
		switch fk {
		case reflect.Slice:
			// A slice, but we need to figure out what the element is so that we can
			// render this properly.
			se := ft.Elem()
			p.Fprintf(&buf, "var s%s []%s\n", fn, se)
			p.Fprintf(&buf, "u%s := %s.([]interface{})\n", fn, eft.expandPrint(sk))
			p.Fprintf(&buf, "for _, v := range u%s {\n", fn)
			if se.Kind() == reflect.Struct {
				// Complex struct that we need to expand
				bs, err := g.expandFprint(sm.Elem, bufSlice)
				if err != nil {
					return bufSlice, err
				}
				bufSlice = bs
				p.Fprintf(&buf, "w = expand%s(v.(map[string]interface{}))\n", strings.Title(se.Name()))
			} else {
				p.Fprintf(&buf, "w := v.(%s)\n", se)
			}
			p.Fprintf(&buf, "s%s = append(s%[1]s, w)\n", fn)
			p.Fprintf(&buf, "}\n")
			p.Fprintf(&buf, "obj.%s = s%[1]s\n", fn)
		case reflect.Struct:
			// Complex resources
			bs, err := g.expandFprint(sm.Elem, bufSlice)
			if err != nil {
				return bufSlice, err
			}
			bufSlice = bs
			et := sm.Elem.Type
			// This is hardcoded for now, as our nested complex resource handling is
			// set to generate a single-element TypeList for complex resources.
			p.Fprintf(&buf, "m%s := %s.([]interface{})[0].(map[string]interface{})\n", fn, eft.expandPrint(sk))
			p.Fprintf(&buf, "obj.%s = expand%s(m%[1]s)\n", fn, strings.Title(et.Name()))
		default:
			p.Fprintf(&buf, "obj.%s = %s.(%s)\n", fn, eft.expandPrint(sk), fk)
		}
	}
	// Close off the function
	p.Fprintf(&buf, "return obj\n")
	p.Fprintf(&buf, "}")
	return bufSlice, nil
}
