package filtergraph

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// filtergraph.schema.json describes the wire format for readers outside Go,
// and nothing but these tests holds it to what the encoder actually writes.
// The two have drifted before: the schema gained the ordered-array form a
// release after MarshalJSON did. So every shape the encoder can reach is
// marshalled here and validated against the schema on disk, and the rules
// the schema states about keys are checked against Validate, which is the
// rule's other home.
//
// The validator below covers the keywords this schema uses and no more; it
// is not a general draft-07 implementation. Adding a keyword to the schema
// without teaching validateSchema about it fails loudly in loadSchema
// rather than passing silently.

// TestSchemaMatchesEncoder marshals every reachable argument shape and
// validates it against the schema.
func TestSchemaMatchesEncoder(t *testing.T) {
	schema := loadSchema(t)

	for _, tc := range []struct {
		name      string
		graph     *FilterGraph
		wantShape string // the "args" member of the first filter
	}{
		{
			name:      "no arguments",
			graph:     graphOf(NewFilter("null")),
			wantShape: "",
		},
		{
			name:      "all named is an object",
			graph:     graphOf(NewFilter("scale").WithArg("w", "1280").WithArg("h", "-2")),
			wantShape: `{"w":"1280","h":"-2"}`,
		},
		{
			name:      "an empty value keeps its key",
			graph:     graphOf(NewFilter("scale").WithArg("flags", "")),
			wantShape: `{"flags":""}`,
		},
		{
			name:      "all positional is an array of strings",
			graph:     graphOf(NewFilter("format").WithPositionalArgs("yuv420p", "yuv444p")),
			wantShape: `["yuv420p","yuv444p"]`,
		},
		{
			name:      "mixed is an array with the named args as objects",
			graph:     graphOf(NewFilter("pad").WithPositionalArgs("1280", "720").WithArg("color", "black")),
			wantShape: `["1280","720",{"color":"black"}]`,
		},
		{
			name:      "a repeated key is an array of one-key objects",
			graph:     graphOf(NewFilter("scale").WithArg("w", "100").WithArg("w", "200")),
			wantShape: `[{"w":"100"},{"w":"200"}]`,
		},
		{
			name:      "raw is the one rendered string",
			graph:     graphOf(NewFilter("pad").WithRawArgs("1280:720:color=black")),
			wantShape: `"1280:720:color=black"`,
		},
		{
			name:      "raw swallows the arguments around it",
			graph:     graphOf(NewFilter("pad").WithPositionalArgs("1280").WithRawArgs("color=black")),
			wantShape: `"1280:color=black"`,
		},
		{
			name:      "an empty argument set is an empty object",
			graph:     graphOf(NewFilter("null").WithNamedArgs(map[string]string{})),
			wantShape: `{}`,
		},
		{
			// Values are escaped at render time, not in the document, so
			// every special character reaches the wire unquoted.
			name:      "values carrying every special character",
			graph:     graphOf(NewFilter("format").WithArg("pix_fmts", `a=b:c,d;e[f]g\h'i j`)),
			wantShape: `{"pix_fmts":"a=b:c,d;e[f]g\\h'i j"}`,
		},
		{
			name:      "positional values carrying every special character",
			graph:     graphOf(NewFilter("format").WithPositionalArgs(`a=b:c,d;e[f]g\h'i j`)),
			wantShape: `["a=b:c,d;e[f]g\\h'i j"]`,
		},
		{
			name:      "an instance and labels",
			graph:     graphOf(NewFilter("scale").WithInstance("main").Input("0:v").Output("out").WithArg("w", "1280")),
			wantShape: `{"w":"1280"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.graph)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			if got := argsMember(t, data); got != tc.wantShape {
				t.Errorf("args member = %s, want %s", got, tc.wantShape)
			}

			if errs := schema.validate(data); len(errs) > 0 {
				t.Errorf("%s does not match filtergraph.schema.json:\n  %s", data, strings.Join(errs, "\n  "))
			}

			// The shape is only worth pinning if it also decodes back to
			// the same filtergraph string.
			var back FilterGraph
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got, want := back.String(), tc.graph.String(); got != want {
				t.Errorf("round-tripped graph = %q, want %q", got, want)
			}
		})
	}
}

// TestSchemaRejectsWhatValidateRejects holds the schema's key rule and
// Validate's to each other. A key ffmpeg cannot be given is rejected in Go
// before it is rendered and in the schema before it is read, and neither
// check is any use if only one of them knows the rule.
func TestSchemaRejectsWhatValidateRejects(t *testing.T) {
	schema := loadSchema(t)

	for _, key := range []string{"a=b", "a:b", "a b", "a,b", "a;b", "a[b]c", `a\b`, "a'b"} {
		t.Run(fmt.Sprintf("key %q", key), func(t *testing.T) {
			f := NewFilter("scale").WithArg(key, "1")

			if err := f.Validate(); err == nil {
				t.Errorf("Validate() = nil, want an error for the key %q", key)
			}

			data, err := json.Marshal(graphOf(f))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if errs := schema.validate(data); len(errs) == 0 {
				t.Errorf("the schema accepts %s, which Validate rejects", data)
			}
		})
	}

	// An empty member name is the one key Validate cannot speak for: an
	// argument with no key is this package's positional argument, so
	// WithArg("", v) builds one rather than a named argument that could be
	// rejected. In a document it is unambiguous, so the decoder rejects it
	// and the schema must too.
	const emptyKey = `{"chains":[{"filters":[{"input_labels":[],"name":"scale","args":{"":"1"},"output_labels":[]}]}]}`
	var g FilterGraph
	if err := json.Unmarshal([]byte(emptyKey), &g); err == nil {
		t.Errorf("Unmarshal(%s) = nil, want an error for an empty argument name", emptyKey)
	}
	if errs := schema.validate([]byte(emptyKey)); len(errs) == 0 {
		t.Errorf("the schema accepts an empty argument name, which the decoder rejects")
	}

	// And the rule stops at keys: the same characters in a value are
	// carried by the escaping, so both checks must accept them.
	f := NewFilter("scale").WithArg("w", `a=b:c,d;e[f]g\h'i j`)
	if err := f.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for a value", err)
	}
	data, err := json.Marshal(graphOf(f))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if errs := schema.validate(data); len(errs) > 0 {
		t.Errorf("the schema rejects a value Validate accepts: %s", strings.Join(errs, "; "))
	}
}

func graphOf(f *Filter) *FilterGraph {
	g := NewFilterGraph()
	g.NewChain().Add(f)
	return g
}

// argsMember returns the raw "args" member of the first filter, or "" when
// the filter carries none.
func argsMember(t *testing.T, data []byte) string {
	t.Helper()
	var doc struct {
		Chains []struct {
			Filters []struct {
				Args json.RawMessage `json:"args"`
			} `json:"filters"`
		} `json:"chains"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal wire: %v", err)
	}
	return string(doc.Chains[0].Filters[0].Args)
}

// ─── a validator for the keywords this schema uses ─────────────────────────

// knownKeywords are the ones validateNode acts on. Anything else in the
// schema is a keyword these tests would silently ignore, so loadSchema
// fails on it instead.
var knownKeywords = map[string]bool{
	"$schema": true, "$ref": true, "$defs": true, "title": true,
	"description": true, "default": true, "type": true, "properties": true,
	"required": true, "additionalProperties": true, "propertyNames": true,
	"items": true, "minItems": true, "pattern": true, "oneOf": true,
}

type schemaDoc struct {
	root map[string]any
}

func loadSchema(t *testing.T) *schemaDoc {
	t.Helper()
	raw, err := os.ReadFile("filtergraph.schema.json")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	s := &schemaDoc{root: root}
	s.checkKeywords(t, root, "#")
	return s
}

// checkKeywords walks the schema and fails on any keyword the validator
// below does not implement, so a rule added to the schema cannot go
// unenforced here.
func (s *schemaDoc) checkKeywords(t *testing.T, node any, path string) {
	t.Helper()
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}
	for k, v := range obj {
		if !knownKeywords[k] {
			t.Fatalf("%s: schema keyword %q is not implemented by this test's validator", path, k)
		}
		switch k {
		case "properties", "$defs":
			for name, sub := range v.(map[string]any) {
				s.checkKeywords(t, sub, path+"/"+k+"/"+name)
			}
		case "items", "propertyNames", "additionalProperties":
			s.checkKeywords(t, v, path+"/"+k)
		case "oneOf":
			for i, sub := range v.([]any) {
				s.checkKeywords(t, sub, fmt.Sprintf("%s/%s/%d", path, k, i))
			}
		}
	}
}

// validate reports every way data fails the schema, empty when it passes.
func (s *schemaDoc) validate(data []byte) []string {
	var inst any
	if err := json.Unmarshal(data, &inst); err != nil {
		return []string{"invalid JSON: " + err.Error()}
	}
	return s.validateNode(s.root, inst, "")
}

func (s *schemaDoc) resolve(ref string) map[string]any {
	name := strings.TrimPrefix(ref, "#/$defs/")
	defs, _ := s.root["$defs"].(map[string]any)
	node, _ := defs[name].(map[string]any)
	return node
}

func (s *schemaDoc) validateNode(node any, inst any, path string) []string {
	obj, ok := node.(map[string]any)
	if !ok {
		// A boolean schema; this schema uses only "additionalProperties":
		// false, which is checked by the caller.
		return nil
	}
	if path == "" {
		path = "$"
	}
	var errs []string
	fail := func(format string, a ...any) {
		errs = append(errs, path+": "+fmt.Sprintf(format, a...))
	}

	if ref, ok := obj["$ref"].(string); ok {
		return s.validateNode(s.resolve(ref), inst, path)
	}

	if want, ok := obj["type"].(string); ok && !hasType(inst, want) {
		fail("want type %s, got %T", want, inst)
		return errs
	}

	if branches, ok := obj["oneOf"].([]any); ok {
		matched := 0
		for _, b := range branches {
			if len(s.validateNode(b, inst, path)) == 0 {
				matched++
			}
		}
		if matched != 1 {
			fail("matched %d of the %d oneOf branches, want exactly 1", matched, len(branches))
		}
	}

	if pattern, ok := obj["pattern"].(string); ok {
		if str, ok := inst.(string); ok && !regexp.MustCompile(pattern).MatchString(str) {
			fail("%q does not match %s", str, pattern)
		}
	}

	switch v := inst.(type) {
	case map[string]any:
		errs = append(errs, s.validateObject(obj, v, path)...)
	case []any:
		errs = append(errs, s.validateArray(obj, v, path)...)
	}
	return errs
}

func (s *schemaDoc) validateObject(obj map[string]any, inst map[string]any, path string) []string {
	var errs []string
	props, _ := obj["properties"].(map[string]any)

	if required, ok := obj["required"].([]any); ok {
		for _, r := range required {
			if _, present := inst[r.(string)]; !present {
				errs = append(errs, fmt.Sprintf("%s: missing required member %q", path, r))
			}
		}
	}

	for name, value := range inst {
		sub, known := props[name]
		if !known {
			switch extra := obj["additionalProperties"].(type) {
			case bool:
				if !extra {
					errs = append(errs, fmt.Sprintf("%s: member %q is not allowed", path, name))
					continue
				}
			case map[string]any:
				sub = extra
			default:
				continue
			}
		}
		if names, ok := obj["propertyNames"]; ok {
			errs = append(errs, s.validateNode(names, name, path+"/"+name+" (name)")...)
		}
		if sub != nil {
			errs = append(errs, s.validateNode(sub, value, path+"/"+name)...)
		}
	}
	return errs
}

func (s *schemaDoc) validateArray(obj map[string]any, inst []any, path string) []string {
	var errs []string
	if min, ok := obj["minItems"].(float64); ok && len(inst) < int(min) {
		errs = append(errs, fmt.Sprintf("%s: %d items, want at least %d", path, len(inst), int(min)))
	}
	if items, ok := obj["items"]; ok {
		for i, elem := range inst {
			errs = append(errs, s.validateNode(items, elem, fmt.Sprintf("%s/%d", path, i))...)
		}
	}
	return errs
}

func hasType(inst any, want string) bool {
	switch want {
	case "object":
		_, ok := inst.(map[string]any)
		return ok
	case "array":
		_, ok := inst.([]any)
		return ok
	case "string":
		_, ok := inst.(string)
		return ok
	case "null":
		return inst == nil
	default:
		panic("unhandled schema type " + want)
	}
}
