package schemagen

import (
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/mitchellh/copystructure"
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

// GenState represents the state of an in-progress schema generation process.
type GenState struct {
	// The current parsed field name being worked on. This will be the final
	// element name in the schema for this field, unless manipulated in a filter.
	CurrentName string

	// The current struct being worked on.
	CurrentField *reflect.StructField

	// The current schema being worked on. This can be updated by a filter
	// in-place to alter the final schema that gets output.
	CurrentSchema *schema.Schema

	// The schema action to undertake for this field.
	Action Action
}

// Copy returns a new copy of this GenState that can be safely
// manipulated without affecting the original.
func (s *GenState) Copy() *GenState {
	ns := new(GenState)
	ns.CurrentName = s.CurrentName
	ns.CurrentField = copystructure.Must(copystructure.Copy(s.CurrentField)).(*reflect.StructField)
	ns.CurrentSchema = copystructure.Must(copystructure.Copy(s.CurrentSchema)).(*schema.Schema)

	return ns
}

// FilterFunc is an optional filter function that can be used to transform a
// passed in GenState, allowing the ability to alter field naming, type
// processing, or the generated schema. See GenState for more information
// on the behaviour of manipulating certain fields.
type FilterFunc func(*GenState) error

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

// schemaFromStruct generates a map[string]*schema.Schema from the supplied
// struct.
func (g *SchemaGenerator) schemaFromStruct(subj interface{}) (map[string]*schema.Schema, error) {
	fields := make(map[string]*schema.Schema)
	gs := new(GenState)

	rawType := u.DereferencePtrType(reflect.TypeOf(subj))
	for i := 0; i < rawType.NumField(); i++ {
		f := rawType.Field(i)
		gs.CurrentField = &f
		gs.CurrentName = u.Underscore(gs.CurrentField.Name)
		gs.CurrentSchema = &schema.Schema{}
		if g.FilterFunc != nil {
			if err := g.FilterFunc(gs); err != nil {
				return fields, err
			}
		}

		// At this stage we should have the data we need to start making decisions
		// about where to go from here:
		//
		// * If the field name or schema were wiped (empty string or nil schema),
		// we skip almost all behaviour except for drilling down into struct
		// fields.
		// * If the current field was wiped, we just skip the field altogether and
		// processing ends here.
		//
		// Otherwise we are good to continue down the normal path - the filter
		// should have transformed the state, if necesasry, to allow for all
		// necessary decisions be made to return the correct schema attribute and
		// put it into the state.
		if gs.Action == ActionSkip {
			continue
		}
		t := u.DereferencePtrType(gs.CurrentField.Type)
		k := t.Kind()
		if gs.CurrentSchema.Type == schema.TypeInvalid {
			gs.CurrentSchema.Type = typeFor(k, t)
			if gs.CurrentSchema.Type == schema.TypeInvalid {
				// Still can't determine the field that we are looking for, skip this
				// one. More than likely it's an interface type that requires further
				// filtering.
				continue
			}
		}
		// We need to perform additional actions for non-primitive types
		switch gs.CurrentSchema.Type {
		case schema.TypeList:
			fallthrough
		case schema.TypeSet:
			switch k {
			case reflect.Slice:
				et := t.Elem().Kind()
				if et != reflect.Struct {
					gs.CurrentSchema.Elem = &schema.Schema{Type: typeFor(t.Elem().Kind(), t.Elem())}
					break
				}
				// If we got this far, this is a set of complex resources. Logic is a
				// simpler subset of the complex nested resource logic below, so we
				// can't fallthrough.
				v := reflect.New(t.Elem()).Elem().Interface()
				e, err := g.schemaFromStruct(v)
				if err != nil {
					return fields, err
				}
				gs.CurrentSchema.Elem = &schema.Resource{Schema: e}
			case reflect.Struct:
				// This is a complex resource! Element assignment is basically the
				// product of recursion from here.
				v := reflect.New(t).Elem().Interface()
				e, err := g.schemaFromStruct(v)
				if err != nil {
					return fields, err
				}
				// If we are promoting then we are actually merging the fields that we
				// got from this iteration into our current field set. This allows
				// things like embedded fields to promote themselves when they would
				// otherwise be unnecessarily nested.
				if gs.Action == ActionPromote {
					for k, v := range e {
						if _, ok := fields[k]; ok {
							return fields, fmt.Errorf("key %q conflicts with key already in the schema. Data: %#v", k, v)
						}
						fields[k] = v
					}
					// Move on to the next field from here to avoid setting the schema twice
					continue
				}
				gs.CurrentSchema.Elem = &schema.Resource{Schema: e}
				gs.CurrentSchema.MaxItems = 1
			}
		}
		if gs.CurrentName != "" {
			fields[gs.CurrentName] = gs.CurrentSchema
		}
	}
	// We are done - return the schema fields.
	return fields, nil
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
// Some level of pretty-printing (with tabbing level dictated by tl) is done
// just to make sure the code is syntatically correct. It should still be
// passed through format.
func (g *SchemaGenerator) schemaFprint(w io.Writer, s map[string]*schema.Schema, tl int) error {
	fmt.Fprintf(w, "map[string]*schema.Schema{\n")
	tl++
	// We need to sort our keys here because map iteration is non-deterministic,
	// making this impossible to test by default.
	var keys []string
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := s[k]
		fmt.Fprintf(w, "%s%q: {\n", strings.Repeat("\t", tl), k)
		tl++
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
				fmt.Fprintf(w, "%sType: schema.%s,\n", strings.Repeat("\t", tl), ri)
			case "Optional":
				fmt.Fprintf(w, "%sOptional: %t,\n", strings.Repeat("\t", tl), ri)
			case "Required":
				fmt.Fprintf(w, "%sRequired: %t,\n", strings.Repeat("\t", tl), ri)
			case "Computed":
				fmt.Fprintf(w, "%sComputed: %t,\n", strings.Repeat("\t", tl), ri)
			case "ForceNew":
				fmt.Fprintf(w, "%sForceNew: %t,\n", strings.Repeat("\t", tl), ri)
			case "Sensitive":
				fmt.Fprintf(w, "%sSensitive: %t,\n", strings.Repeat("\t", tl), ri)
			case "Description":
				fmt.Fprintf(w, "%sDescription: %q,\n", strings.Repeat("\t", tl), ri)
			case "Deprecated":
				fmt.Fprintf(w, "%sDeprecated: %q,\n", strings.Repeat("\t", tl), ri)
			case "Removed":
				fmt.Fprintf(w, "%sRemoved: %q,\n", strings.Repeat("\t", tl), ri)
			case "MinItems":
				fmt.Fprintf(w, "%sMinItems: %d,\n", strings.Repeat("\t", tl), ri)
			case "MaxItems":
				fmt.Fprintf(w, "%sMaxItems: %d,\n", strings.Repeat("\t", tl), ri)
			case "Default":
				fmt.Fprintf(w, "%sDefault: %s,\n", strings.Repeat("\t", tl), valToStr(ri))
			case "InputDefault":
				fmt.Fprintf(w, "%sInputDefault: %s,\n", strings.Repeat("\t", tl), valToStr(ri))
			case "Elem":
				switch t := ri.(type) {
				case *schema.Schema:
					fmt.Fprintf(w, "%sElem: &schema.Schema{Type: schema.%s},\n", strings.Repeat("\t", tl), t.Type)
				case *schema.Resource:
					fmt.Fprintf(w, "%sElem: &schema.Resource{\n", strings.Repeat("\t", tl))
					tl++
					fmt.Fprintf(w, "%sSchema: ", strings.Repeat("\t", tl))
					g.schemaFprint(w, t.Schema, tl)
					tl--
					fmt.Fprintf(w, "%s},\n", strings.Repeat("\t", tl))
				}
			}
		}
		tl--
		fmt.Fprintf(w, "%s},\n", strings.Repeat("\t", tl))
	}
	tl--
	fmt.Fprintf(w, "%s}", strings.Repeat("\t", tl))
	if tl > 0 {
		fmt.Fprintf(w, ",")
	}
	fmt.Fprintf(w, "\n")

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
