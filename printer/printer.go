// Package print performs general file I/O tasks for the various generators.
package print

import (
	"bytes"
	"fmt"
	"go/format"
	"io"
	"log"
	"os"
)

// srcHeaderFormat is the header lines for each source file generated with this
// tool.
const srcHeaderFormat = `
// This file is auto-generated - DO NOT EDIT
package %s
`

// Generator is an interface that the various generators implement. This allows
// the printer to be able to work with the various generators in a general way.
type Generator interface {
	// Run is an atomic generator writing operation - it encompasses all of the
	// actions necessary to internally generate the data needed for output, and
	// then generating that output. The data is sent to the passed in io.Writer.
	Run(io.Writer) error

	// Filename outputs the filename the generator wants the output placed in.
	Filename() string
}

// Printer stores the configuration for the generation and printing operations.
type Printer struct {
	// The name of the package this operation is generating.
	PkgName string

	// The path of the package. Must be a directory. This is relative to the
	// directory the generation operation is being conducted in. If the directory
	// doesn't exist, it is created.
	PkgDir string

	// A set of generators. These are run, with the files generated bearing the
	// package name above with the file
	Generators []Generator
}

// Run runs the printer operation.
func (p *Printer) Run() error {
	log.Printf("Beginning generation operation for package %q", p.PkgName)
	stat, err := os.Stat(p.PkgDir)
	switch {
	case err == nil:
		if !stat.Mode().IsDir() {
			return fmt.Errorf("%q exists and is not a directory", p.PkgDir)
		}
	case err != nil && os.IsNotExist(err):
		if err := os.MkdirAll(p.PkgDir, 0777); err != nil {
			return fmt.Errorf("cannot create directory %q: %s", p.PkgDir, err)
		}
		log.Printf("Created directory: %q", p.PkgDir)
	case err != nil:
		return fmt.Errorf("could not stat dir %q: %s", p.PkgDir, err)
	}

	for _, g := range p.Generators {
		fp := fmt.Sprintf("%s/%s", p.PkgDir, g.Filename())
		log.Printf("Generating %q...\n", fp)
		fd, err := os.Create(fp)
		defer fd.Close()
		if err != nil {
			return fmt.Errorf("could not open file %q: %s", fp, err)
		}
		var buf bytes.Buffer

		if _, err := fmt.Fprintf(&buf, srcHeaderFormat, p.PkgName); err != nil {
			return fmt.Errorf("error writing output header to buffer: %s", err)
		}
		if err := g.Run(&buf); err != nil {
			return fmt.Errorf("error generating source: %s", err)
		}
		out, err := format.Source(buf.Bytes())
		if err != nil {
			return fmt.Errorf("error formatting source: %s", err)
		}
		if _, err := fd.Write(out); err != nil {
			return fmt.Errorf("error writing output: %s", err)
		}
	}
	return nil
}
