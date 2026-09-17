// Command gen generates ffprobe/types.gen.go and ffprobe/ffprobe.schema.json.
//
// Three inputs feed it, each an authority on one thing:
//
//   - doc/ffprobe.xsd from every FFmpeg tag listed in ffprobe.versions is the
//     field vocabulary: which fields exist, their base type, and which
//     release lines carry them. The XSDs are unioned, so the generated types
//     accept the output of every listed version.
//   - testdata/probe/<version>/sections.txt (the output of `ffprobe
//     -sections`) is the structure: which sections are arrays, which carry
//     variable keys, and which print an XML-only "type" attribute.
//   - testdata/probe/<version>/*.json captures are the type oracle: they
//     confirm which fields ffprobe actually emits in JSON, catch fields the
//     XSD dropped but the JSON writer still prints, and surface keys the
//     XSD never described.
//
// The XSD describes ffprobe's XML writer. Its JSON writer differs in ways
// the XSD cannot express (integers versus numeric strings, tags as objects,
// side data as free-form objects), which is why every scalar is emitted as
// one of the lenient value types in values.go.
//
// Run from the ffprobe package directory: go generate ./ffprobe
package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

const (
	versionsFile = "ffprobe.versions"
	xsdDir       = "xsd"
	probeDir     = "testdata/probe"
	goOut        = "types.gen.go"
	schemaOut    = "ffprobe.schema.json"
	xsdURL       = "https://raw.githubusercontent.com/FFmpeg/FFmpeg/refs/tags/%s/doc/ffprobe.xsd"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run() error {
	tags, err := readVersions()
	if err != nil {
		return err
	}
	m := newModel(tags)
	for _, tag := range tags {
		path := filepath.Join(xsdDir, "ffprobe-"+tag+".xsd")
		if err := ensureXSD(path, tag); err != nil {
			return err
		}
		raw, err := parseXSD(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := m.merge(tag, raw); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}

	sections, err := readSections()
	if err != nil {
		return err
	}
	m.classify(sections)

	obs, err := observeCaptures(m)
	if err != nil {
		return err
	}
	m.applyObservations(obs)

	src, err := m.renderGo()
	if err != nil {
		return err
	}
	if err := os.WriteFile(goOut, src, 0o644); err != nil {
		return err
	}
	schema, err := m.renderSchema()
	if err != nil {
		return err
	}
	if err := os.WriteFile(schemaOut, schema, 0o644); err != nil {
		return err
	}

	for _, w := range obs.warnings() {
		fmt.Fprintln(os.Stderr, "gen: warning:", w)
	}
	fmt.Fprintf(os.Stderr, "gen: %d types, %d captured versions, wrote %s and %s\n",
		len(m.emitOrder()), len(obs.versions), goOut, schemaOut)
	return nil
}

// ─── inputs ────────────────────────────────────────────────────────────────

func readVersions() ([]string, error) {
	b, err := os.ReadFile(versionsFile)
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tags = append(tags, line)
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("%s lists no tags", versionsFile)
	}
	return tags, nil
}

func ensureXSD(path, tag string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	url := fmt.Sprintf(xsdURL, tag)
	fmt.Fprintf(os.Stderr, "gen: downloading %s\n", url)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Minimal XSD model: ffprobe.xsd only uses complexType with attributes,
// a sequence or a choice of elements, and no inheritance.
type xsdSchema struct {
	ComplexTypes []xsdComplexType `xml:"complexType"`
}

type xsdComplexType struct {
	Name       string       `xml:"name,attr"`
	Sequence   *xsdParticle `xml:"sequence"`
	Choice     *xsdParticle `xml:"choice"`
	Attributes []xsdAttr    `xml:"attribute"`
}

type xsdParticle struct {
	MaxOccurs string       `xml:"maxOccurs,attr"`
	Elements  []xsdElement `xml:"element"`
}

type xsdElement struct {
	Name      string `xml:"name,attr"`
	Type      string `xml:"type,attr"`
	MaxOccurs string `xml:"maxOccurs,attr"`
}

type xsdAttr struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
	Use  string `xml:"use,attr"`
}

type rawType struct {
	name   string
	choice bool
	fields []rawField
}

type rawField struct {
	name      string
	typ       string // local XSD type name, e.g. "int", "string", "streamType"
	attr      bool
	unbounded bool
	required  bool
}

func parseXSD(path string) ([]rawType, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s xsdSchema
	if err := xml.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	var out []rawType
	for _, ct := range s.ComplexTypes {
		rt := rawType{name: ct.Name}
		for _, a := range ct.Attributes {
			rt.fields = append(rt.fields, rawField{name: a.Name, typ: local(a.Type), attr: true, required: a.Use == "required"})
		}
		particle := ct.Sequence
		if ct.Choice != nil {
			particle = ct.Choice
			rt.choice = true
		}
		if particle != nil {
			for _, e := range particle.Elements {
				rt.fields = append(rt.fields, rawField{
					name:      e.Name,
					typ:       local(e.Type),
					unbounded: e.MaxOccurs == "unbounded" || particle.MaxOccurs == "unbounded",
				})
			}
		}
		out = append(out, rt)
	}
	return out, nil
}

func local(qname string) string {
	if i := strings.LastIndex(qname, ":"); i >= 0 {
		return qname[i+1:]
	}
	return qname
}

// sectionInfo is what `ffprobe -sections` tells us about a section name.
type sectionInfo struct {
	variable bool // ..V. : carries arbitrary keys
	hasType  bool // ...T : XML writer adds a type attribute
	array    bool // .A.. : JSON array
}

var sectionLine = regexp.MustCompile(`^([W.][A.][V.][T.])\s+(\S+)`)

// readSections unions the section flags from every captured version.
func readSections() (map[string]sectionInfo, error) {
	files, _ := filepath.Glob(filepath.Join(probeDir, "*", "sections.txt"))
	out := map[string]sectionInfo{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(b), "\n") {
			mm := sectionLine.FindStringSubmatch(line)
			if mm == nil {
				continue
			}
			flags := mm[1]
			name := mm[2]
			if i := strings.Index(name, "/"); i >= 0 {
				name = name[:i]
			}
			si := out[name]
			si.array = si.array || flags[1] == 'A'
			si.variable = si.variable || flags[2] == 'V'
			si.hasType = si.hasType || flags[3] == 'T'
			out[name] = si
		}
	}
	if len(out) == 0 {
		fmt.Fprintf(os.Stderr, "gen: warning: no %s/*/sections.txt found; variable sections unknown\n", probeDir)
	}
	return out, nil
}

// ─── model ─────────────────────────────────────────────────────────────────

type kind int

const (
	kindStruct  kind = iota // regular object
	kindDatum               // {key, value} pair; XML representation of a map entry
	kindMap                 // wrapper around datum entries → Tags
	kindWrapper             // single unbounded element → array
	kindChoice              // choice of element types → array of union
)

type typeDef struct {
	xsd      string
	goName   string
	kind     kind
	fields   []*field
	byName   map[string]*field
	elemType string // for kindWrapper: the element's XSD type
	choices  []string
	sections []string // section names that reference this type
	variable bool
	hasType  bool
	since    string
}

type field struct {
	json      string
	xsdType   string
	attr      bool
	unbounded bool
	since     string
	until     string // last tag whose XSD lists it; "" if present in newest
	observed  string // newest captured version that emitted it
	stillJSON bool   // dropped from the XSD but still printed by the JSON writer
	drop      bool
	doc       string
}

type model struct {
	tags  []string
	types map[string]*typeDef
	order []string
}

func newModel(tags []string) *model {
	return &model{tags: tags, types: map[string]*typeDef{}}
}

func (m *model) newest() string { return m.tags[len(m.tags)-1] }

func (m *model) merge(tag string, raw []rawType) error {
	seen := map[string]bool{}
	for _, rt := range raw {
		td := m.types[rt.name]
		if td == nil {
			td = &typeDef{xsd: rt.name, byName: map[string]*field{}, since: tag}
			m.types[rt.name] = td
			m.order = append(m.order, rt.name)
		}
		if rt.choice {
			td.kind = kindChoice
		}
		for _, rf := range rt.fields {
			f := td.byName[rf.name]
			if f == nil {
				f = &field{json: rf.name, xsdType: rf.typ, attr: rf.attr, unbounded: rf.unbounded, since: tag}
				td.byName[rf.name] = f
				td.fields = append(td.fields, f)
			} else if f.xsdType != rf.typ {
				return fmt.Errorf("%s.%s changed type %s → %s in %s", rt.name, rf.name, f.xsdType, rf.typ, tag)
			}
			f.until = tag
			f.unbounded = f.unbounded || rf.unbounded
			seen[rt.name+"."+rf.name] = true
		}
	}
	return nil
}

// jsonOnlyFields are keys the JSON writer prints that no XSD lists. Each is
// confirmed by the captured output; the generator warns when a capture shows
// a key that is in neither the XSDs nor this table.
var jsonOnlyFields = map[string][]jsonOnly{
	"streamGroupType": {{name: "id", typ: "string", since: "n7.0.2"}},
	"streamType": {
		{name: "view_ids_available", typ: "string", since: "n7.1.3"},
		{name: "view_pos_available", typ: "string", since: "n7.1.3"},
		{name: "ts_id", typ: "int"},           // MPEG-TS transport stream id
		{name: "ts_packetsize", typ: "int"},   // MPEG-TS packet size
		{name: "is_avc", typ: "boolean"},      // H.264 in AVCC (length-prefixed) form
		{name: "nal_length_size", typ: "int"}, // H.264 AVCC NAL length prefix size
	},
}

type jsonOnly struct{ name, typ, since string }

// fieldNameOverrides replaces the derived Go name for specific fields. The
// integer "duration" of frames and packets is a tick count, so it takes the
// TS suffix streams already use (duration_ts) and leaves Duration() free
// for the time.Duration helper.
var fieldNameOverrides = map[string]string{
	"errorType.string":    "Message",
	"frameType.duration":  "DurationTS",
	"packetType.duration": "DurationTS",
}

// fieldTypeOverrides replaces the derived Go type for specific fields. The
// error code is an AVERROR value with named constants in errors.go.
var fieldTypeOverrides = map[string]string{
	"errorType.code": "AVError",
}

// secondsSuffix names every field ffprobe prints as decimal seconds. The
// Go name drops ffprobe's own "_time" suffix and adds "Secs" ("duration"
// and "duration_time" both become DurationSecs, "start_time" becomes
// StartSecs), which leaves Duration() and StartTime() free for the
// time.Duration helpers and pairs each field with its integer tick
// counterpart (DurationTS, StartPTS, PTS).
const secondsSuffix = "Secs"

// boolTypes and boolFields mark xsd:int fields that are really 0/1 flags.
var boolTypes = map[string]bool{"streamDispositionType": true, "pixelFormatFlagsType": true}
var boolFields = map[string]bool{
	"key_frame": true, "interlaced_frame": true, "top_field_first": true, "lossless": true,
}

// classify decides how each XSD type maps to JSON, using section flags.
func (m *model) classify(sections map[string]sectionInfo) {
	for typ, extra := range jsonOnlyFields {
		td := m.types[typ]
		if td == nil {
			continue
		}
		for _, rf := range extra {
			if td.byName[rf.name] != nil {
				continue
			}
			f := &field{json: rf.name, xsdType: rf.typ, attr: true, since: rf.since, doc: "Not listed in the XSD; observed in JSON output."}
			td.byName[rf.name] = f
			td.fields = append(td.fields, f)
		}
	}
	newest := m.newest()
	for _, td := range m.types {
		for _, f := range td.fields {
			if f.until == newest {
				f.until = ""
			}
		}
	}
	isDatum := func(td *typeDef) bool {
		return len(td.fields) == 2 && td.byName["key"] != nil && td.byName["value"] != nil
	}
	for _, td := range m.types {
		if isDatum(td) {
			td.kind = kindDatum
		}
	}
	for _, td := range m.types {
		if td.kind == kindChoice {
			for _, f := range td.fields {
				td.choices = append(td.choices, f.xsdType)
			}
			continue
		}
		if td.kind == kindDatum {
			continue
		}
		if len(td.fields) == 1 && !td.fields[0].attr && td.fields[0].unbounded {
			elem := m.types[td.fields[0].xsdType]
			switch {
			case elem != nil && elem.kind == kindDatum && td.xsd == "tagsType":
				td.kind = kindMap
			case elem != nil && elem.kind == kindDatum:
				// A variable section with no fixed keys (side data pieces,
				// stream group blocks): a struct that is all Extra.
			default:
				td.kind = kindWrapper
				td.elemType = td.fields[0].xsdType
			}
		}
	}
	// Record which section names reference each type, so the section flags
	// (keyed by section name) can be applied to types.
	for _, td := range m.types {
		for _, f := range td.fields {
			if f.attr {
				continue
			}
			if ref := m.types[f.xsdType]; ref != nil {
				name := f.json
				if td.kind == kindWrapper || td.kind == kindChoice {
					// e.g. streamsType.stream → section "stream"
					name = f.json
				}
				ref.sections = append(ref.sections, name)
			}
		}
	}
	for _, td := range m.types {
		for _, s := range td.sections {
			si := sections[s]
			td.variable = td.variable || si.variable
			td.hasType = td.hasType || si.hasType
		}
		// Datum arrays are how XML spells a variable section's keys; in
		// JSON those keys are inline, so the field goes and Extra takes over.
		for _, f := range td.fields {
			ref := m.types[f.xsdType]
			if ref == nil || ref.kind != kindDatum {
				continue
			}
			f.drop = true
			if f.json == "tag" {
				// Before 6.1 the XSD spelled the tags dictionary as repeated
				// <tag key= value=> elements directly on the parent; JSON has
				// always printed a "tags" object.
				if t := td.byName["tags"]; t != nil && cmpVersion(f.since, t.since) < 0 {
					t.since = f.since
				}
				continue
			}
			td.variable = true
		}
		// The XML writer stamps a "type" attribute on T sections; the JSON
		// writer does not.
		if td.hasType {
			if f := td.byName["type"]; f != nil && f.attr {
				f.drop = true
			}
		}
	}
	m.assignGoNames()
}

var goNameOverrides = map[string]string{
	"ffprobeType":           "Result",
	"errorType":             "ProbeError",
	"packetSideDataType":    "SideData",
	"streamDispositionType": "Disposition",
	"tagsType":              "Tags",
}

func (m *model) assignGoNames() {
	for _, td := range m.types {
		if n, ok := goNameOverrides[td.xsd]; ok {
			td.goName = n
			continue
		}
		n := strings.TrimSuffix(td.xsd, "Type")
		td.goName = strings.ToUpper(n[:1]) + n[1:]
	}
}

// emitted reports whether a type becomes a Go struct.
func (td *typeDef) emitted() bool {
	switch td.kind {
	case kindStruct:
		return td.xsd != "subtitleType" // merged into Frame
	default:
		return false
	}
}

func (m *model) emitOrder() []*typeDef {
	var out []*typeDef
	for _, name := range m.order {
		if td := m.types[name]; td.emitted() {
			out = append(out, td)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].goName < out[j].goName })
	return out
}

// fieldsFor returns the fields a Go struct gets, merging subtitleType into
// frameType. Scalars come first, then nested sections.
func (m *model) fieldsFor(td *typeDef) []*field {
	var scalars, nested []*field
	add := func(fs []*field, doc string) {
		for _, f := range fs {
			if f.drop {
				continue
			}
			g := *f
			if doc != "" {
				g.doc = doc
			}
			if g.attr {
				scalars = append(scalars, &g)
			} else {
				nested = append(nested, &g)
			}
		}
	}
	add(td.fields, "")
	if td.xsd == "frameType" {
		if st := m.types["subtitleType"]; st != nil {
			seen := map[string]bool{}
			for _, f := range td.fields {
				seen[f.json] = true
			}
			var extra []*field
			for _, f := range st.fields {
				if !seen[f.json] {
					extra = append(extra, f)
				}
			}
			add(extra, "Subtitle frames only.")
		}
	}
	return append(scalars, nested...)
}

// ─── observations from captured JSON ───────────────────────────────────────

type observations struct {
	versions []string
	seen     map[string]map[string]string // type → field → newest version observed
	first    map[string]map[string]string // type → field → oldest version observed
	unknown  map[string]map[string][]string
}

func (o *observations) observedIn(typ, key, ver string) bool {
	return o.first[typ] != nil && o.first[typ][key] == ver
}

func observeCaptures(m *model) (*observations, error) {
	obs := &observations{seen: map[string]map[string]string{}, first: map[string]map[string]string{}, unknown: map[string]map[string][]string{}}
	dirs, _ := filepath.Glob(filepath.Join(probeDir, "*"))
	for _, dir := range dirs {
		ver := filepath.Base(dir)
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		if len(files) == 0 {
			continue
		}
		obs.versions = append(obs.versions, ver)
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			if len(bytes.TrimSpace(b)) == 0 {
				continue // option unsupported by that ffprobe version
			}
			var doc any
			if err := json.Unmarshal(b, &doc); err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			obs.walk(m, m.types["ffprobeType"], doc, ver, filepath.Base(f))
		}
	}
	sortVersions(obs.versions)
	return obs, nil
}

func (o *observations) walk(m *model, td *typeDef, v any, ver, file string) {
	o.walkItem(m, td, v, ver, file, false)
}

// walkItem walks one object; inChoice marks members of packets_and_frames,
// where the JSON writer adds a "type" discriminator that is not a field.
func (o *observations) walkItem(m *model, td *typeDef, v any, ver, file string, inChoice bool) {
	obj, ok := v.(map[string]any)
	if !ok || td == nil {
		return
	}
	for key, val := range obj {
		if inChoice && key == "type" {
			continue
		}
		f := td.byName[key]
		if td.xsd == "frameType" && f == nil {
			if st := m.types["subtitleType"]; st != nil {
				f = st.byName[key]
			}
		}
		if f == nil || f.drop {
			if !td.variable {
				if o.unknown[td.xsd] == nil {
					o.unknown[td.xsd] = map[string][]string{}
				}
				o.unknown[td.xsd][key] = appendUnique(o.unknown[td.xsd][key], ver)
			}
			continue
		}
		if o.seen[td.xsd] == nil {
			o.seen[td.xsd] = map[string]string{}
		}
		if cmpVersion(ver, o.seen[td.xsd][key]) > 0 {
			o.seen[td.xsd][key] = ver
		}
		if o.first[td.xsd] == nil {
			o.first[td.xsd] = map[string]string{}
		}
		if prev := o.first[td.xsd][key]; prev == "" || cmpVersion(ver, prev) < 0 {
			o.first[td.xsd][key] = ver
		}
		ref := m.types[f.xsdType]
		if ref == nil {
			continue
		}
		switch ref.kind {
		case kindStruct:
			o.walk(m, ref, val, ver, file)
		case kindWrapper:
			for _, item := range asArray(val) {
				o.walk(m, m.types[ref.elemType], item, ver, file)
			}
		case kindChoice:
			for _, item := range asArray(val) {
				o.walkItem(m, m.choiceTarget(ref, item), item, ver, file, true)
			}
		}
	}
}

// choiceTarget picks the member type of a choice array for one JSON item.
func (m *model) choiceTarget(ref *typeDef, item any) *typeDef {
	obj, _ := item.(map[string]any)
	if t, _ := obj["type"].(string); t == "packet" {
		return m.types["packetType"]
	}
	if mt, _ := obj["media_type"].(string); mt == "subtitle" && len(ref.choices) > 0 {
		// subtitleType is merged into frameType for emission; walk as frame
		// so both field sets are recognised.
		return m.types["frameType"]
	}
	return m.types["frameType"]
}

func asArray(v any) []any {
	arr, _ := v.([]any)
	return arr
}

func (o *observations) warnings() []string {
	var out []string
	for typ, keys := range o.unknown {
		for key, vers := range keys {
			out = append(out, fmt.Sprintf("%s.%s emitted by ffprobe %s but absent from every XSD", typ, key, strings.Join(vers, ", ")))
		}
	}
	sort.Strings(out)
	return out
}

func (m *model) applyObservations(o *observations) {
	for _, td := range m.types {
		for _, f := range td.fields {
			if f.since == "" {
				// JSON-only field with no known origin: date it from captures.
				for _, ver := range o.versions {
					if o.seen[td.xsd][f.json] != "" && o.observedIn(td.xsd, f.json, ver) {
						f.since = ver
						break
					}
				}
			}
			f.observed = o.seen[td.xsd][f.json]
			if f.until != "" && f.observed != "" && cmpVersion(f.observed, tagVersion(f.until)) > 0 {
				f.stillJSON = true
			}
		}
	}
}

// ─── naming and typing ─────────────────────────────────────────────────────

var acronyms = map[string]string{"id": "ID", "pts": "PTS", "dts": "DTS", "ts": "TS", "b": "B", "sar": "SAR", "dar": "DAR"}

func goFieldName(jsonName string) string {
	var sb strings.Builder
	for _, tok := range strings.Split(jsonName, "_") {
		if tok == "" {
			continue
		}
		if a, ok := acronyms[tok]; ok {
			sb.WriteString(a)
			continue
		}
		sb.WriteString(strings.ToUpper(tok[:1]))
		sb.WriteString(tok[1:])
	}
	return sb.String()
}

var rationalFields = map[string]bool{
	"r_frame_rate":         true,
	"avg_frame_rate":       true,
	"time_base":            true,
	"sample_aspect_ratio":  true,
	"display_aspect_ratio": true,
}

// goType returns the Go type expression for a field and the JSON tag option.
//
// Every array section is a slice of pointers so a range loop can call the
// pointer-receiver helpers directly. A nested object is a value (its fields
// all report Valid, so the zero value is honest and never panics); the only
// pointer is the "error" section, whose presence is the signal.
func (m *model) goType(td *typeDef, f *field) (typ, tagOpt string) {
	if t, ok := fieldTypeOverrides[td.xsd+"."+f.json]; ok {
		return t, "omitzero"
	}
	if f.attr {
		if boolTypes[td.xsd] || boolFields[f.json] {
			return "Bool", "omitzero"
		}
		switch f.xsdType {
		case "int", "long", "integer", "short", "unsignedInt", "unsignedLong":
			return "Int", "omitzero"
		case "float", "double", "decimal":
			return "Seconds", "omitzero"
		case "boolean":
			return "Bool", "omitzero"
		default:
			if rationalFields[f.json] {
				return "Rat", "omitzero"
			}
			return "string", "omitempty"
		}
	}
	ref := m.types[f.xsdType]
	if ref == nil {
		return "json.RawMessage", "omitempty"
	}
	switch ref.kind {
	case kindMap:
		return "Tags", "omitempty"
	case kindWrapper:
		elem := m.types[ref.elemType]
		return "[]*" + elem.goName, "omitempty"
	case kindChoice:
		if f.json == "packets_and_frames" {
			return "[]PacketOrFrame", "omitempty"
		}
		return "[]*Frame", "omitempty"
	case kindStruct:
		if f.unbounded {
			return "[]*" + ref.goName, "omitempty"
		}
		if ref.xsd == "errorType" {
			return "*" + ref.goName, "omitempty"
		}
		return ref.goName, "omitzero"
	}
	return "json.RawMessage", "omitempty"
}

// ─── rendering ─────────────────────────────────────────────────────────────

func (m *model) renderGo() ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by ffprobe/internal/gen from doc/ffprobe.xsd at FFmpeg tags %s. DO NOT EDIT.\n\n", strings.Join(m.tags, ", "))
	b.WriteString("package ffprobe\n\nimport \"encoding/json\"\n\n")
	fmt.Fprintf(&b, "// SchemaVersions lists the FFmpeg tags whose ffprobe.xsd fed the generated types.\nvar SchemaVersions = []string{%s}\n\n", quoteList(m.tags))

	for _, td := range m.emitOrder() {
		fields := m.fieldsFor(td)
		fmt.Fprintf(&b, "// %s is ffprobe's %s.", td.goName, td.xsd)
		if td.since != m.tags[0] {
			fmt.Fprintf(&b, " Added in FFmpeg %s.", tagVersion(td.since))
		}
		if td.xsd == "frameType" {
			b.WriteString("\n// Subtitle frames (media_type \"subtitle\") use the same struct; their\n// fields are marked below.")
		}
		if td.variable {
			b.WriteString("\n//\n// ffprobe prints additional keys here that vary by content; they are\n// collected in Extra.")
		}
		fmt.Fprintf(&b, "\ntype %s struct {\n", td.goName)
		var known []string
		for _, f := range fields {
			typ, opt := m.goType(td, f)
			known = append(known, f.json)
			b.WriteString("\t" + fieldDoc(td, f, m) + "\n")
			fmt.Fprintf(&b, "\t%s %s `json:\"%s,%s\"`\n\n", m.fieldName(td, f), typ, f.json, opt)
		}
		if td.variable {
			b.WriteString("\t// Extra holds every key ffprobe printed that is not a named field above.\n")
			b.WriteString("\tExtra map[string]any `json:\"-\"`\n")
		}
		b.WriteString("}\n\n")
		if td.variable {
			renderVariableMethods(&b, td.goName, known)
		}
	}
	return format.Source(b.Bytes())
}

func (m *model) fieldName(td *typeDef, f *field) string {
	if n, ok := fieldNameOverrides[td.xsd+"."+f.json]; ok {
		return n
	}
	name := goFieldName(f.json)
	if typ, _ := m.goType(td, f); typ == "Seconds" {
		name = strings.TrimSuffix(name, "Time") + secondsSuffix
	}
	return name
}

func fieldDoc(td *typeDef, f *field, m *model) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("// %s is the JSON \"%s\" field.", m.fieldName(td, f), f.json))
	if f.doc != "" {
		parts = append(parts, f.doc)
	}
	if f.since != "" && f.since != m.tags[0] {
		parts = append(parts, fmt.Sprintf("Added in FFmpeg %s.", tagVersion(f.since)))
	}
	if f.until != "" {
		if f.stillJSON {
			parts = append(parts, fmt.Sprintf("Dropped from the XSD after FFmpeg %s but still printed in JSON.", tagVersion(f.until)))
		} else {
			parts = append(parts, fmt.Sprintf("Removed after FFmpeg %s.", tagVersion(f.until)))
		}
	}
	return strings.Join(parts, " ")
}

func renderVariableMethods(b *bytes.Buffer, name string, known []string) {
	fmt.Fprintf(b, "var %sKnownKeys = map[string]bool{%s}\n\n", lowerFirst(name), quoteSet(known))
	fmt.Fprintf(b, `// UnmarshalJSON decodes the named fields and stores every other key in Extra.
func (v *%[1]s) UnmarshalJSON(data []byte) error {
	type plain %[1]s
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	extra, err := decodeExtra(data, %[2]sKnownKeys)
	if err != nil {
		return err
	}
	*v = %[1]s(p)
	v.Extra = extra
	return nil
}

// MarshalJSON emits the named fields followed by the keys in Extra.
func (v %[1]s) MarshalJSON() ([]byte, error) {
	type plain %[1]s
	return encodeExtra(plain(v), v.Extra)
}

`, name, lowerFirst(name))
}

func (m *model) renderSchema() ([]byte, error) {
	root := &jsonschema.Schema{
		Schema:      "http://json-schema.org/draft-07/schema#",
		Ref:         "#/$defs/Result",
		Description: fmt.Sprintf("ffprobe -print_format json output, unioned over FFmpeg %s. Numeric fields accept both JSON numbers and numeric strings because ffprobe's JSON writer prints most of them as strings.", strings.Join(m.tags, ", ")),
		Defs:        map[string]*jsonschema.Schema{},
	}
	for _, td := range m.emitOrder() {
		s := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}}
		if td.variable {
			s.AdditionalProperties = &jsonschema.Schema{}
		}
		for _, f := range m.fieldsFor(td) {
			s.Properties[f.json] = m.fieldSchema(td, f)
		}
		root.Defs[td.goName] = s
	}
	root.Defs["PacketOrFrame"] = &jsonschema.Schema{
		OneOf: []*jsonschema.Schema{
			{AllOf: []*jsonschema.Schema{{Ref: "#/$defs/Packet"}, {Type: "object", Properties: map[string]*jsonschema.Schema{"type": {Enum: []any{"packet"}}}}}},
			{AllOf: []*jsonschema.Schema{{Ref: "#/$defs/Frame"}, {Type: "object", Properties: map[string]*jsonschema.Schema{"type": {Enum: []any{"frame", "subtitle"}}}}}},
		},
	}
	root.Defs["Tags"] = &jsonschema.Schema{Type: "object", AdditionalProperties: &jsonschema.Schema{Type: "string"}}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (m *model) fieldSchema(td *typeDef, f *field) *jsonschema.Schema {
	var s *jsonschema.Schema
	typ, _ := m.goType(td, f)
	switch {
	case typ == "Int":
		s = &jsonschema.Schema{Types: []string{"integer", "string"}}
	case typ == "AVError":
		s = &jsonschema.Schema{Types: []string{"integer", "string"}, Description: "AVERROR code: a negated POSIX errno or an FFmpeg FFERRTAG value."}
	case typ == "Seconds":
		s = &jsonschema.Schema{Types: []string{"number", "string"}}
	case typ == "Bool":
		s = &jsonschema.Schema{Types: []string{"integer", "boolean"}}
	case typ == "Rat":
		s = &jsonschema.Schema{Type: "string", Pattern: `^(-?[0-9]+[/:]-?[0-9]+|N/A)$`}
	case typ == "string":
		s = &jsonschema.Schema{Type: "string"}
	case typ == "Tags":
		s = &jsonschema.Schema{Ref: "#/$defs/Tags"}
	case strings.HasPrefix(typ, "[]"):
		s = &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Ref: "#/$defs/" + strings.TrimPrefix(typ[2:], "*")}}
	case strings.HasPrefix(typ, "*"):
		s = &jsonschema.Schema{Ref: "#/$defs/" + typ[1:]}
	case typ != "json.RawMessage":
		s = &jsonschema.Schema{Ref: "#/$defs/" + typ}
	default:
		s = &jsonschema.Schema{}
	}
	var notes []string
	if f.doc != "" {
		notes = append(notes, f.doc)
	}
	if f.since != "" && f.since != m.tags[0] {
		notes = append(notes, "Added in FFmpeg "+tagVersion(f.since)+".")
	}
	if f.until != "" && !f.stillJSON {
		notes = append(notes, "Removed after FFmpeg "+tagVersion(f.until)+".")
	}
	if len(notes) > 0 {
		s.Description = strings.Join(notes, " ")
	}
	return s
}

// ─── helpers ───────────────────────────────────────────────────────────────

func quoteList(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(q, ", ")
}

func quoteSet(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = fmt.Sprintf("%q: true", s)
	}
	return strings.Join(q, ", ")
}

func lowerFirst(s string) string { return strings.ToLower(s[:1]) + s[1:] }

// tagVersion turns "n7.1.3" into "7.1".
func tagVersion(tag string) string {
	v := strings.TrimPrefix(tag, "n")
	parts := strings.Split(v, ".")
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, ".")
}

var versionRe = regexp.MustCompile(`\d+`)

func versionParts(v string) []int {
	var out []int
	for _, s := range versionRe.FindAllString(v, -1) {
		n := 0
		fmt.Sscanf(s, "%d", &n)
		out = append(out, n)
	}
	return out
}

func cmpVersion(a, b string) int {
	if a == "" || b == "" {
		if a == b {
			return 0
		}
		if a == "" {
			return -1
		}
		return 1
	}
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func sortVersions(v []string) {
	sort.Slice(v, func(i, j int) bool { return cmpVersion(v[i], v[j]) < 0 })
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

var _ = errors.New
