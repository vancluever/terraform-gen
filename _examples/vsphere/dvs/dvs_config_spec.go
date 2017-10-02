//go:generate go run dvs_config_spec.go
package main

import (
	"log"

	print "github.com/radeksimko/terraform-gen/printer"
	"github.com/radeksimko/terraform-gen/schemagen"
	"github.com/vmware/govmomi/vim25/types"
)

func main() {
	p := &print.Printer{
		PkgName: "dvs",
		PkgDir:  "output",
		Generators: []print.Generator{
			&schemagen.SchemaGenerator{
				Obj:          types.DVSCreateSpec{},
				File:         "dvs_create_spec.go",
				VariableName: "schemaDVSCreateSpec",
				FilterFunc:   filterDVSCreateSpec,
			},
		},
	}
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func filterDVSCreateSpec(gs *schemagen.GenState) error {
	return nil
}
