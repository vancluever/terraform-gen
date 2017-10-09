package util

import (
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
)

func Underscore(name string) string {
	var words []string
	var camelCase = regexp.MustCompile("(^[^A-Z]*|[A-Z]*)([A-Z][^A-Z]+|$)")

	for _, submatch := range camelCase.FindAllStringSubmatch(name, -1) {
		if submatch[1] != "" {
			words = append(words, submatch[1])
		}
		if submatch[2] != "" {
			words = append(words, submatch[2])
		}
	}

	return strings.ToLower(strings.Join(words, "_"))
}

func DereferencePtrType(t reflect.Type) reflect.Type {
	kind := t.Kind()
	if kind == reflect.Ptr {
		return DereferencePtrType(t.Elem())
	}
	return t
}

func DereferencePtrValue(v reflect.Value) reflect.Value {
	kind := v.Kind()
	if kind == reflect.Ptr {
		return DereferencePtrValue(v.Elem())
	}
	return v
}

// TabWriter is an I/O filter that adds indentation to a stream.
type TabPrinter struct {
	// The underlying io.Writer that TabWriter passes the modified output off to.
	x io.Writer

	// The tab count.
	tc int

	// A flag to keep track if a outstanding line is open - that is, if there is
	// currently a non-newline-terminated section of the stream that is being
	// processed that has an outstanding opening brace.
	lo bool
	nl bool
}

// NewTabWriter creates a new TabWriter at the specified indentation level,
// bound to the supplied io.Writer.
func NewTabWriter(x io.Writer, tc int) *TabWriter {
	p := &TabPrinter{x: x, tc: tc}
	return p
}

// Write modifies the I/O stream sent to it with proper indentation, based on
// the opening and closing braces in the content.
func (w *TabWriter) Write(p []byte) (int, error) {
	var buf []byte
	var count int
	for i, c := range p {
		switch c {
		case '\n':

			n, err := x.Write(buf)
			if err != nil {
				return err
			}
			count+= n
		}
		if new && c != '\n' {
			p = append(p[:n], append(strings.Repeat("\t", tc) + p[n:]
		}
	}
}


// TabPrinter is a simple printer that keeps track of indentation.
type TabPrinter struct {
	// The tab count.
	tc int

	// An internal line buffer that calculate uses in case the processed string
	// does not end in a newline. Note that this line does not contain the
	// tabbing and is not suitable for printing.
	lbuf string
}

// NewTabPrinter creates a new TabPrinter at the specified indentation level.
func NewTabPrinter(tc int) *TabPrinter {
	p := &TabPrinter{tc: tc}
	return p
}

// Count returns the current tab (indent count).
func (p *TabPrinter) Count() int {
	return p.tc
}

// Fprintf passes off to fmt.Fprintf, with pretty printing for indents and
// tracking of current indent level.
func (p *TabPrinter) Fprintf(w io.Writer, format string, a ...interface{}) (n int, err error) {
	// Render the string first.
	rendered := fmt.Sprintf(format, a...)
	p.calcClose(rendered)
	tc := p.tc
	if len(p.lbuf) > 0 {
		// No tabbing if we are in the middle of printing an unbroken line
		tc = 0
	}
	n, err = fmt.Fprintf(w, "%s%s", strings.Repeat("\t", tc), rendered)
	p.calcOpen(rendered)
	return
}

// calcClose calculates if we need to unindent. This is denoted by a closing
// brace before a opening brace. This decrements in the indent.
func (p *TabPrinter) calcClose(s string) {
	for _, c := range s {
		switch c {
		case '{', '(':
			// Opening brace, no unindent
			return
		case '}', ')':
			// Closing brace, unindent and return
			p.tc--
			if p.tc < 0 {
				p.tc = 0
			}
			return
		}
	}
	return
}

// calcOpen calculates if we need to indent. This is denoted by a positive
// number of opening braces versus closing braces. If there is no trailing
// newline, we buffer the line for later checking and don't indent at all.
func (p *TabPrinter) calcOpen(s string) {
	var ob, cb int
	p.lbuf = p.lbuf + s
	if !strings.HasSuffix(p.lbuf, "\n") {
		// Don't process
		return
	}
	for _, c := range p.lbuf {
		switch c {
		case '{', '(':
			ob++
		case '}', ')':
			cb++
		}
	}
	if ob > cb {
		p.tc++
	}
	p.lbuf = ""
	return
}
