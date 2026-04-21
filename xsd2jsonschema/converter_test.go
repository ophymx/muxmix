package xsd2jsonschema_test

import (
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/ophymx/muxmix/xsd2jsonschema"
)

// wrap returns a minimal xs:schema wrapper around a body snippet.
func wrap(body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="http://example.com/test"
           xmlns:t="http://example.com/test">` +
		body + `</xs:schema>`
}

func mustConvert(t *testing.T, xsd string) *jsonschema.Schema {
	t.Helper()
	s, err := xsd2jsonschema.Convert(strings.NewReader(xsd))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return s
}

// ── root schema ─────────────────────────────────────────────────────────────

func TestRootRef(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:element name="root" type="t:rootType"/>
		<xs:complexType name="rootType"/>
	`))
	if s.Ref != "#/$defs/rootType" {
		t.Errorf("$ref = %q, want #/$defs/rootType", s.Ref)
	}
	if s.Schema != xsd2jsonschema.Draft07SchemaURI {
		t.Errorf("$schema = %q, want %s", s.Schema, xsd2jsonschema.Draft07SchemaURI)
	}
}

func TestDefsPopulated(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:complexType name="aType"/>
		<xs:complexType name="bType"/>
	`))
	for _, name := range []string{"aType", "bType"} {
		if _, ok := s.Defs[name]; !ok {
			t.Errorf("$defs missing %q", name)
		}
	}
}

// ── attribute handling ───────────────────────────────────────────────────────

func TestRequiredAttribute(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:complexType name="fmtType">
			<xs:attribute name="filename"  type="xs:string"  use="required"/>
			<xs:attribute name="nb_streams" type="xs:int" use="required"/>
			<xs:attribute name="bit_rate"   type="xs:long"/>
		</xs:complexType>
	`))
	ct := s.Defs["fmtType"]
	if ct == nil {
		t.Fatal("$defs[fmtType] is nil")
	}

	// required list
	wantRequired := map[string]bool{"filename": true, "nb_streams": true}
	for _, r := range ct.Required {
		delete(wantRequired, r)
	}
	if len(wantRequired) > 0 {
		t.Errorf("missing required attrs: %v", wantRequired)
	}
	for _, r := range ct.Required {
		if r == "bit_rate" {
			t.Errorf("optional attribute bit_rate must not appear in required")
		}
	}

	// property types
	if ct.Properties["filename"].Type != "string" {
		t.Errorf("filename type = %q, want string", ct.Properties["filename"].Type)
	}
	if ct.Properties["nb_streams"].Type != "integer" {
		t.Errorf("nb_streams type = %q, want integer", ct.Properties["nb_streams"].Type)
	}
	if ct.Properties["bit_rate"].Type != "integer" {
		t.Errorf("bit_rate type = %q, want integer (xs:long)", ct.Properties["bit_rate"].Type)
	}
}

// ── XSD primitive type mapping ───────────────────────────────────────────────

func TestPrimitiveTypeMapping(t *testing.T) {
	cases := []struct {
		xsdType  string
		wantType string
		wantFmt  string
	}{
		{"xs:string", "string", ""},
		{"xs:int", "integer", ""},
		{"xs:integer", "integer", ""},
		{"xs:long", "integer", ""},
		{"xs:short", "integer", ""},
		{"xs:float", "number", ""},
		{"xs:double", "number", ""},
		{"xs:decimal", "number", ""},
		{"xs:boolean", "boolean", ""},
		{"xs:anyURI", "string", "uri"},
		{"xs:dateTime", "string", "date-time"},
		{"xs:date", "string", "date"},
	}

	for _, tc := range cases {
		t.Run(tc.xsdType, func(t *testing.T) {
			xsd := wrap(`<xs:complexType name="t">
				<xs:attribute name="v" type="` + tc.xsdType + `" use="required"/>
			</xs:complexType>`)
			s := mustConvert(t, xsd)
			prop := s.Defs["t"].Properties["v"]
			if prop.Type != tc.wantType {
				t.Errorf("type = %q, want %q", prop.Type, tc.wantType)
			}
			if prop.Format != tc.wantFmt {
				t.Errorf("format = %q, want %q", prop.Format, tc.wantFmt)
			}
		})
	}
}

// ── sequence / occurrence handling ──────────────────────────────────────────

func TestSequenceRequiredElement(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:complexType name="parent">
			<xs:sequence>
				<xs:element name="must"  type="t:childType"/>
				<xs:element name="maybe" type="t:childType" minOccurs="0" maxOccurs="1"/>
			</xs:sequence>
		</xs:complexType>
		<xs:complexType name="childType"/>
	`))
	ct := s.Defs["parent"]
	if ct == nil {
		t.Fatal("$defs[parent] is nil")
	}

	// "must" should be required, "maybe" should not
	reqSet := make(map[string]bool)
	for _, r := range ct.Required {
		reqSet[r] = true
	}
	if !reqSet["must"] {
		t.Error("element 'must' (minOccurs=1) should be required")
	}
	if reqSet["maybe"] {
		t.Error("element 'maybe' (minOccurs=0) must not be required")
	}
}

func TestUnboundedElementBecomesArray(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:complexType name="listType">
			<xs:sequence>
				<xs:element name="item" type="t:itemType" minOccurs="0" maxOccurs="unbounded"/>
			</xs:sequence>
		</xs:complexType>
		<xs:complexType name="itemType"/>
	`))
	ct := s.Defs["listType"]
	if ct == nil {
		t.Fatal("$defs[listType] is nil")
	}
	prop := ct.Properties["item"]
	if prop.Type != "array" {
		t.Fatalf("item property type = %q, want array", prop.Type)
	}
	if prop.Items == nil {
		t.Fatal("item.items is nil")
	}
	if prop.Items.Ref != "#/$defs/itemType" {
		t.Errorf("item.items.$ref = %q, want #/$defs/itemType", prop.Items.Ref)
	}
}

func TestBoundedArrayMinItems(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:complexType name="requiredList">
			<xs:sequence>
				<xs:element name="item" type="xs:string" minOccurs="1" maxOccurs="unbounded"/>
			</xs:sequence>
		</xs:complexType>
	`))
	prop := s.Defs["requiredList"].Properties["item"]
	if prop.Type != "array" {
		t.Fatalf("type = %q, want array", prop.Type)
	}
	if prop.MinItems == nil || *prop.MinItems != 1 {
		t.Errorf("minItems = %v, want 1", prop.MinItems)
	}
}

// ── choice handling ──────────────────────────────────────────────────────────

func TestChoiceElementsNotRequired(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:complexType name="framesType">
			<xs:choice minOccurs="0" maxOccurs="unbounded">
				<xs:element name="frame"    type="t:frameType"/>
				<xs:element name="subtitle" type="t:subtitleType"/>
			</xs:choice>
		</xs:complexType>
		<xs:complexType name="frameType"/>
		<xs:complexType name="subtitleType"/>
	`))
	ct := s.Defs["framesType"]
	if ct == nil {
		t.Fatal("$defs[framesType] is nil")
	}
	// Neither element should be required.
	for _, r := range ct.Required {
		if r == "frame" || r == "subtitle" {
			t.Errorf("choice element %q must not be required", r)
		}
	}
	// Both properties should exist.
	if ct.Properties["frame"] == nil {
		t.Error("expected properties.frame")
	}
	if ct.Properties["subtitle"] == nil {
		t.Error("expected properties.subtitle")
	}
}

// ── simpleType handling ──────────────────────────────────────────────────────

func TestSimpleTypeEnumeration(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:simpleType name="colorEnum">
			<xs:restriction base="xs:string">
				<xs:enumeration value="red"/>
				<xs:enumeration value="green"/>
				<xs:enumeration value="blue"/>
			</xs:restriction>
		</xs:simpleType>
	`))
	st := s.Defs["colorEnum"]
	if st == nil {
		t.Fatal("$defs[colorEnum] is nil")
	}
	if len(st.Enum) != 3 {
		t.Fatalf("enum len = %d, want 3", len(st.Enum))
	}
	want := map[any]bool{"red": true, "green": true, "blue": true}
	for _, v := range st.Enum {
		delete(want, v)
	}
	if len(want) > 0 {
		t.Errorf("missing enum values: %v", want)
	}
}

func TestSimpleTypePatternRestriction(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:simpleType name="isoDate">
			<xs:restriction base="xs:string">
				<xs:pattern value="\d{4}-\d{2}-\d{2}"/>
			</xs:restriction>
		</xs:simpleType>
	`))
	st := s.Defs["isoDate"]
	if st == nil {
		t.Fatal("$defs[isoDate] is nil")
	}
	if st.Pattern != `\d{4}-\d{2}-\d{2}` {
		t.Errorf("pattern = %q, want \\d{4}-\\d{2}-\\d{2}", st.Pattern)
	}
}

func TestSimpleTypeStringLength(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:simpleType name="boundedString">
			<xs:restriction base="xs:string">
				<xs:minLength value="1"/>
				<xs:maxLength value="100"/>
			</xs:restriction>
		</xs:simpleType>
	`))
	st := s.Defs["boundedString"]
	if st == nil {
		t.Fatal("$defs[boundedString] is nil")
	}
	if st.MinLength == nil || *st.MinLength != 1 {
		t.Errorf("minLength = %v, want 1", st.MinLength)
	}
	if st.MaxLength == nil || *st.MaxLength != 100 {
		t.Errorf("maxLength = %v, want 100", st.MaxLength)
	}
}

func TestSimpleTypeNumericRange(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:simpleType name="positiveScore">
			<xs:restriction base="xs:integer">
				<xs:minInclusive value="0"/>
				<xs:maxInclusive value="100"/>
			</xs:restriction>
		</xs:simpleType>
	`))
	st := s.Defs["positiveScore"]
	if st == nil {
		t.Fatal("$defs[positiveScore] is nil")
	}
	if st.Minimum == nil || *st.Minimum != 0 {
		t.Errorf("minimum = %v, want 0", st.Minimum)
	}
	if st.Maximum == nil || *st.Maximum != 100 {
		t.Errorf("maximum = %v, want 100", st.Maximum)
	}
}

// ── complexContent extension ─────────────────────────────────────────────────

func TestComplexContentExtension(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:complexType name="base">
			<xs:attribute name="id" type="xs:int" use="required"/>
		</xs:complexType>
		<xs:complexType name="derived">
			<xs:complexContent>
				<xs:extension base="t:base">
					<xs:attribute name="name" type="xs:string" use="required"/>
				</xs:extension>
			</xs:complexContent>
		</xs:complexType>
	`))
	ct := s.Defs["derived"]
	if ct == nil {
		t.Fatal("$defs[derived] is nil")
	}
	if len(ct.AllOf) != 2 {
		t.Fatalf("allOf len = %d, want 2", len(ct.AllOf))
	}
	if ct.AllOf[0].Ref != "#/$defs/base" {
		t.Errorf("allOf[0].$ref = %q, want #/$defs/base", ct.AllOf[0].Ref)
	}
}

// ── cross-type $ref ──────────────────────────────────────────────────────────

func TestCrossTypeRef(t *testing.T) {
	s := mustConvert(t, wrap(`
		<xs:complexType name="outer">
			<xs:sequence>
				<xs:element name="inner" type="t:innerType" minOccurs="0"/>
			</xs:sequence>
		</xs:complexType>
		<xs:complexType name="innerType"/>
	`))
	prop := s.Defs["outer"].Properties["inner"]
	if prop == nil {
		t.Fatal("properties.inner is nil")
	}
	if prop.Ref != "#/$defs/innerType" {
		t.Errorf("$ref = %q, want #/$defs/innerType", prop.Ref)
	}
}

// ── ffprobe XSD (representative subset) ─────────────────────────────────────

const ffprobeSubsetXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xsd:schema xmlns:xsd="http://www.w3.org/2001/XMLSchema"
            targetNamespace="http://www.ffmpeg.org/schema/ffprobe"
            xmlns:ffprobe="http://www.ffmpeg.org/schema/ffprobe">

  <xsd:element name="ffprobe" type="ffprobe:ffprobeType"/>

  <xsd:complexType name="ffprobeType">
    <xsd:sequence>
      <xsd:element name="streams" type="ffprobe:streamsType" minOccurs="0" maxOccurs="1"/>
      <xsd:element name="format"  type="ffprobe:formatType"  minOccurs="0" maxOccurs="1"/>
    </xsd:sequence>
  </xsd:complexType>

  <xsd:complexType name="streamsType">
    <xsd:sequence>
      <xsd:element name="stream" type="ffprobe:streamType" minOccurs="0" maxOccurs="unbounded"/>
    </xsd:sequence>
  </xsd:complexType>

  <xsd:complexType name="streamType">
    <xsd:attribute name="index"      type="xsd:int"    use="required"/>
    <xsd:attribute name="codec_name" type="xsd:string"/>
    <xsd:attribute name="codec_type" type="xsd:string"/>
    <xsd:attribute name="bit_rate"   type="xsd:int"/>
    <xsd:attribute name="duration"   type="xsd:float"/>
    <xsd:attribute name="width"      type="xsd:int"/>
    <xsd:attribute name="height"     type="xsd:int"/>
  </xsd:complexType>

  <xsd:complexType name="formatType">
    <xsd:attribute name="filename"    type="xsd:string" use="required"/>
    <xsd:attribute name="nb_streams"  type="xsd:int"    use="required"/>
    <xsd:attribute name="format_name" type="xsd:string" use="required"/>
    <xsd:attribute name="duration"    type="xsd:float"/>
    <xsd:attribute name="size"        type="xsd:long"/>
    <xsd:attribute name="bit_rate"    type="xsd:long"/>
  </xsd:complexType>

</xsd:schema>`

func TestFFprobeSubset_Root(t *testing.T) {
	s := mustConvert(t, ffprobeSubsetXSD)
	if s.Ref != "#/$defs/ffprobeType" {
		t.Errorf("$ref = %q, want #/$defs/ffprobeType", s.Ref)
	}
}

func TestFFprobeSubset_StreamsIsArray(t *testing.T) {
	s := mustConvert(t, ffprobeSubsetXSD)
	streamsType := s.Defs["streamsType"]
	if streamsType == nil {
		t.Fatal("streamsType missing from $defs")
	}
	streamProp := streamsType.Properties["stream"]
	if streamProp == nil {
		t.Fatal("streamsType.properties.stream is nil")
	}
	if streamProp.Type != "array" {
		t.Errorf("stream property type = %q, want array", streamProp.Type)
	}
	if streamProp.Items == nil || streamProp.Items.Ref != "#/$defs/streamType" {
		t.Errorf("stream items.$ref = %v, want #/$defs/streamType", streamProp.Items)
	}
}

func TestFFprobeSubset_FormatRequired(t *testing.T) {
	s := mustConvert(t, ffprobeSubsetXSD)
	ft := s.Defs["formatType"]
	if ft == nil {
		t.Fatal("formatType missing from $defs")
	}
	reqSet := make(map[string]bool)
	for _, r := range ft.Required {
		reqSet[r] = true
	}
	for _, want := range []string{"filename", "nb_streams", "format_name"} {
		if !reqSet[want] {
			t.Errorf("required missing %q", want)
		}
	}
	for _, notWant := range []string{"duration", "size", "bit_rate"} {
		if reqSet[notWant] {
			t.Errorf("optional attr %q must not be required", notWant)
		}
	}
}

func TestFFprobeSubset_StreamTypeProperties(t *testing.T) {
	s := mustConvert(t, ffprobeSubsetXSD)
	st := s.Defs["streamType"]
	if st == nil {
		t.Fatal("streamType missing from $defs")
	}
	cases := []struct{ name, wantType string }{
		{"index", "integer"},
		{"codec_name", "string"},
		{"duration", "number"},
		{"width", "integer"},
	}
	for _, tc := range cases {
		p := st.Properties[tc.name]
		if p == nil {
			t.Errorf("properties.%s is nil", tc.name)
			continue
		}
		if p.Type != tc.wantType {
			t.Errorf("properties.%s type = %q, want %q", tc.name, p.Type, tc.wantType)
		}
	}
}
