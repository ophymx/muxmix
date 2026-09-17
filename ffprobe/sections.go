package ffprobe

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Section is one node of the tree ffprobe -sections prints: the structure
// of its output for the running binary. Use it to feature-detect at
// runtime, for example whether stream_groups exist.
type Section struct {
	Name       string // e.g. "tags"
	UniqueName string // e.g. "stream_tags"; equals Name when ffprobe prints no alias
	Wrapper    bool   // W: contains other sections, no local entries
	Array      bool   // A: an array of same-typed children
	Variable   bool   // V: carries arbitrary keys
	Typed      bool   // T: XML writer adds a type attribute (ffprobe 6.1+)
	Children   []*Section
}

// Find returns the first section with the given name or unique name in
// depth-first order, or nil.
func (s *Section) Find(name string) *Section {
	if s == nil {
		return nil
	}
	if s.Name == name || s.UniqueName == name {
		return s
	}
	for _, c := range s.Children {
		if found := c.Find(name); found != nil {
			return found
		}
	}
	return nil
}

// Has reports whether Find would succeed.
func (s *Section) Has(name string) bool { return s.Find(name) != nil }

// Walk calls fn for every section in depth-first order with its depth.
func (s *Section) Walk(fn func(sec *Section, depth int)) {
	s.walk(fn, 0)
}

func (s *Section) walk(fn func(*Section, int), depth int) {
	fn(s, depth)
	for _, c := range s.Children {
		c.walk(fn, depth+1)
	}
}

var sectionLineRe = regexp.MustCompile(`^([W.])([A.])([V.])([T.])?(\s+)(\S+)`)

// ParseSections parses ffprobe -sections output into its root section.
func ParseSections(text string) (*Section, error) {
	type frame struct {
		indent int
		sec    *Section
	}
	var root *Section
	var stack []frame
	for _, line := range strings.Split(text, "\n") {
		m := sectionLineRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil || m[6] == "=" { // legend lines read "W... = Section is a wrapper"
			continue
		}
		name, unique := m[6], m[6]
		if a, b, ok := strings.Cut(m[6], "/"); ok {
			name, unique = a, b
		}
		sec := &Section{
			Name:       name,
			UniqueName: unique,
			Wrapper:    m[1] == "W",
			Array:      m[2] == "A",
			Variable:   m[3] == "V",
			Typed:      m[4] == "T",
		}
		indent := len(m[5])
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			if root != nil {
				return nil, fmt.Errorf("ffprobe: sections output has more than one root")
			}
			root = sec
		} else {
			parent := stack[len(stack)-1].sec
			parent.Children = append(parent.Children, sec)
		}
		stack = append(stack, frame{indent: indent, sec: sec})
	}
	if root == nil {
		return nil, fmt.Errorf("ffprobe: no sections found in output")
	}
	return root, nil
}

// Sections runs ffprobe -sections and returns the section tree.
func (p *Prober) Sections(ctx context.Context) (*Section, error) {
	raw, err := p.runBare(ctx, "-sections")
	if err != nil {
		return nil, err
	}
	return ParseSections(string(raw))
}
