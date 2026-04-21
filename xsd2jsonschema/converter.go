// Package xsd2jsonschema converts XML Schema Definitions (XSD) to JSON Schema (draft-07).
package xsd2jsonschema

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

const Draft07SchemaURI = "http://json-schema.org/draft-07/schema#"

// xsd primitive → JSON Schema type mapping.
type primitiveInfo struct {
	typ    string
	format string
}

var builtins = map[string]primitiveInfo{
	// strings
	"string":           {typ: "string"},
	"normalizedString": {typ: "string"},
	"token":            {typ: "string"},
	"language":         {typ: "string"},
	"Name":             {typ: "string"},
	"NCName":           {typ: "string"},
	"ID":               {typ: "string"},
	"IDREF":            {typ: "string"},
	"IDREFS":           {typ: "string"},
	"NMTOKEN":          {typ: "string"},
	"NMTOKENS":         {typ: "string"},
	"anyURI":           {typ: "string", format: "uri"},
	"anySimpleType":    {typ: "string"},
	"anyType":          {typ: "string"},
	"hexBinary":        {typ: "string"},
	"base64Binary":     {typ: "string"},
	// date / time
	"dateTime":   {typ: "string", format: "date-time"},
	"date":       {typ: "string", format: "date"},
	"time":       {typ: "string", format: "time"},
	"duration":   {typ: "string"},
	"gYear":      {typ: "string"},
	"gYearMonth": {typ: "string"},
	"gMonth":     {typ: "string"},
	"gMonthDay":  {typ: "string"},
	"gDay":       {typ: "string"},
	// boolean
	"boolean": {typ: "boolean"},
	// integers
	"int":                {typ: "integer"},
	"integer":            {typ: "integer"},
	"long":               {typ: "integer"},
	"short":              {typ: "integer"},
	"byte":               {typ: "integer"},
	"unsignedInt":        {typ: "integer"},
	"unsignedLong":       {typ: "integer"},
	"unsignedShort":      {typ: "integer"},
	"unsignedByte":       {typ: "integer"},
	"positiveInteger":    {typ: "integer"},
	"nonPositiveInteger": {typ: "integer"},
	"negativeInteger":    {typ: "integer"},
	"nonNegativeInteger": {typ: "integer"},
	// numbers
	"decimal": {typ: "number"},
	"float":   {typ: "number"},
	"double":  {typ: "number"},
}

// converter holds working state for a single conversion.
type converter struct {
	defs map[string]*jsonschema.Schema
}

// Convert parses an XSD from r and returns the equivalent JSON Schema (draft-07).
func Convert(r io.Reader) (*jsonschema.Schema, error) {
	var s xsdSchema
	if err := xml.NewDecoder(r).Decode(&s); err != nil {
		return nil, fmt.Errorf("xsd2jsonschema: parse xsd: %w", err)
	}
	c := &converter{defs: make(map[string]*jsonschema.Schema)}
	return c.convert(&s)
}

func (c *converter) convert(s *xsdSchema) (*jsonschema.Schema, error) {
	// simpleTypes first so complexTypes can reference them.
	for _, st := range s.SimpleTypes {
		if st.Name != "" {
			c.defs[st.Name] = c.convertSimpleType(st)
		}
	}
	for _, ct := range s.ComplexTypes {
		if ct.Name != "" {
			c.defs[ct.Name] = c.convertComplexType(ct)
		}
	}

	root := &jsonschema.Schema{Schema: Draft07SchemaURI}
	if len(c.defs) > 0 {
		root.Defs = c.defs
	}
	if len(s.Elements) == 0 {
		return root, nil
	}

	if el := s.Elements[0]; el.Type != "" {
		root.Ref = "#/$defs/" + localName(el.Type)
	} else if el.Name != "" {
		root.Ref = "#/$defs/" + el.Name
	}
	return root, nil
}

// convertComplexType converts an xs:complexType to a JSON Schema object.
func (c *converter) convertComplexType(ct *xsdComplexType) *jsonschema.Schema {
	// Handle xs:complexContent (inheritance).
	if ct.ComplexContent != nil {
		return c.convertComplexContent(ct.ComplexContent)
	}
	// Handle xs:simpleContent (value + attributes).
	if ct.SimpleContent != nil {
		return c.convertSimpleContent(ct.SimpleContent)
	}

	s := &jsonschema.Schema{Type: "object", Properties: make(map[string]*jsonschema.Schema)}
	var required []string

	for _, attr := range ct.Attributes {
		s.Properties[attr.Name] = c.typeRefSchema(attr.Type)
		if attr.Use == "required" {
			required = append(required, attr.Name)
		}
	}
	if ct.Sequence != nil {
		reqs := c.addParticle(ct.Sequence, s.Properties, false)
		required = append(required, reqs...)
	}
	if ct.Choice != nil {
		c.addParticle(ct.Choice, s.Properties, true)
	}
	if ct.All != nil {
		reqs := c.addParticle(ct.All, s.Properties, false)
		required = append(required, reqs...)
	}

	if len(required) > 0 {
		s.Required = required
	}
	if len(s.Properties) == 0 {
		s.Properties = nil
	}
	return s
}

// convertComplexContent handles xs:complexContent (extension / restriction).
func (c *converter) convertComplexContent(cc *xsdComplexContent) *jsonschema.Schema {
	if cc.Extension != nil {
		ext := cc.Extension
		extObj := &jsonschema.Schema{Type: "object", Properties: make(map[string]*jsonschema.Schema)}
		var required []string

		for _, attr := range ext.Attributes {
			extObj.Properties[attr.Name] = c.typeRefSchema(attr.Type)
			if attr.Use == "required" {
				required = append(required, attr.Name)
			}
		}
		if ext.Sequence != nil {
			reqs := c.addParticle(ext.Sequence, extObj.Properties, false)
			required = append(required, reqs...)
		}
		if ext.Choice != nil {
			c.addParticle(ext.Choice, extObj.Properties, true)
		}
		if len(required) > 0 {
			extObj.Required = required
		}
		if len(extObj.Properties) == 0 {
			extObj.Properties = nil
		}
		return &jsonschema.Schema{AllOf: []*jsonschema.Schema{
			{Ref: "#/$defs/" + localName(ext.Base)},
			extObj,
		}}
	}
	if cc.Restriction != nil {
		r := cc.Restriction
		obj := &jsonschema.Schema{Type: "object", Properties: make(map[string]*jsonschema.Schema)}
		var required []string
		for _, attr := range r.Attributes {
			obj.Properties[attr.Name] = c.typeRefSchema(attr.Type)
			if attr.Use == "required" {
				required = append(required, attr.Name)
			}
		}
		if r.Sequence != nil {
			reqs := c.addParticle(r.Sequence, obj.Properties, false)
			required = append(required, reqs...)
		}
		if len(required) > 0 {
			obj.Required = required
		}
		return obj
	}
	return &jsonschema.Schema{Type: "object"}
}

// convertSimpleContent handles xs:simpleContent (base type + attributes).
func (c *converter) convertSimpleContent(sc *xsdSimpleContent) *jsonschema.Schema {
	if sc.Extension == nil {
		return &jsonschema.Schema{Type: "string"}
	}
	ext := sc.Extension
	// The text value is represented as a "value" property of the base type.
	s := &jsonschema.Schema{Type: "object", Properties: make(map[string]*jsonschema.Schema)}
	s.Properties["value"] = c.typeRefSchema(ext.Base)
	var required []string
	for _, attr := range ext.Attributes {
		s.Properties[attr.Name] = c.typeRefSchema(attr.Type)
		if attr.Use == "required" {
			required = append(required, attr.Name)
		}
	}
	if len(required) > 0 {
		s.Required = required
	}
	return s
}

// convertSimpleType converts an xs:simpleType to a JSON Schema.
func (c *converter) convertSimpleType(st *xsdSimpleType) *jsonschema.Schema {
	if st.List != nil {
		return &jsonschema.Schema{Type: "array", Items: c.typeRefSchema(st.List.ItemType)}
	}
	if st.Union != nil {
		var schemas []*jsonschema.Schema
		for member := range strings.FieldsSeq(st.Union.MemberTypes) {
			schemas = append(schemas, c.typeRefSchema(member))
		}
		if len(schemas) == 1 {
			return schemas[0]
		}
		return &jsonschema.Schema{AnyOf: schemas}
	}
	if st.Restriction != nil {
		return c.convertRestriction(st.Restriction)
	}
	return &jsonschema.Schema{Type: "string"}
}

func (c *converter) convertRestriction(r *xsdRestriction) *jsonschema.Schema {
	base := c.typeRefSchema(r.Base)
	s := *base // copy

	if len(r.Enumerations) > 0 {
		vals := make([]any, len(r.Enumerations))
		for i, e := range r.Enumerations {
			vals[i] = e.Value
		}
		s.Enum = vals
		return &s
	}
	if r.Pattern != nil {
		s.Pattern = r.Pattern.Value
	}
	if r.MinLength != nil {
		if n, err := strconv.Atoi(r.MinLength.Value); err == nil {
			s.MinLength = &n
		}
	}
	if r.MaxLength != nil {
		if n, err := strconv.Atoi(r.MaxLength.Value); err == nil {
			s.MaxLength = &n
		}
	}
	if r.Length != nil {
		if n, err := strconv.Atoi(r.Length.Value); err == nil {
			s.MinLength = &n
			s.MaxLength = &n
		}
	}
	if r.MinInclusive != nil {
		if f, err := strconv.ParseFloat(r.MinInclusive.Value, 64); err == nil {
			s.Minimum = &f
		}
	}
	if r.MaxInclusive != nil {
		if f, err := strconv.ParseFloat(r.MaxInclusive.Value, 64); err == nil {
			s.Maximum = &f
		}
	}
	if r.MinExclusive != nil {
		if f, err := strconv.ParseFloat(r.MinExclusive.Value, 64); err == nil {
			s.ExclusiveMinimum = &f
		}
	}
	if r.MaxExclusive != nil {
		if f, err := strconv.ParseFloat(r.MaxExclusive.Value, 64); err == nil {
			s.ExclusiveMaximum = &f
		}
	}
	return &s
}

// addParticle adds elements from a xs:sequence, xs:choice or xs:all to props.
// isChoice suppresses required tracking. Returns names of required elements.
func (c *converter) addParticle(p *xsdParticle, props map[string]*jsonschema.Schema, isChoice bool) []string {
	// A choice or sequence with maxOccurs="unbounded" wraps each child element in an array.
	particleIsArray := p.MaxOccurs == "unbounded" || maxOccursInt(p.MaxOccurs) > 1

	var required []string
	for _, el := range p.Elements {
		var s *jsonschema.Schema
		if particleIsArray {
			inner := c.elementItemSchema(el)
			s = &jsonschema.Schema{Type: "array", Items: inner}
		} else {
			s = c.elementSchema(el)
		}
		props[el.Name] = s
		if !isChoice && !particleIsArray && isRequiredElement(el) {
			required = append(required, el.Name)
		}
	}
	// Recurse into nested sequences.
	for _, seq := range p.Sequences {
		reqs := c.addParticle(seq, props, isChoice)
		if !isChoice {
			required = append(required, reqs...)
		}
	}
	// Nested choices: elements are never required.
	for _, ch := range p.Choices {
		c.addParticle(ch, props, true)
	}
	return required
}

// elementSchema returns a schema for an xs:element occurrence, wrapping in
// an array schema when maxOccurs > 1 or "unbounded".
func (c *converter) elementSchema(el *xsdElement) *jsonschema.Schema {
	if el.MaxOccurs == "unbounded" || maxOccursInt(el.MaxOccurs) > 1 {
		inner := c.elementItemSchema(el)
		arr := &jsonschema.Schema{Type: "array", Items: inner}
		if isRequiredElement(el) {
			one := 1
			arr.MinItems = &one
		}
		return arr
	}
	return c.elementItemSchema(el)
}

// elementItemSchema returns the base (non-array) schema for an xs:element.
func (c *converter) elementItemSchema(el *xsdElement) *jsonschema.Schema {
	if el.Type != "" {
		return c.typeRefSchema(el.Type)
	}
	if el.ComplexType != nil {
		return c.convertComplexType(el.ComplexType)
	}
	if el.SimpleType != nil {
		return c.convertSimpleType(el.SimpleType)
	}
	if el.Ref != "" {
		return &jsonschema.Schema{Ref: "#/$defs/" + localName(el.Ref)}
	}
	return &jsonschema.Schema{Type: "object"}
}

// typeRefSchema returns a JSON Schema for a given XSD type name (e.g. "xsd:int",
// "xs:string", "ns:myType").
func (c *converter) typeRefSchema(typeName string) *jsonschema.Schema {
	if typeName == "" {
		return &jsonschema.Schema{Type: "string"}
	}
	prefix := namePrefix(typeName)
	local := localName(typeName)

	// xsd / xs prefixed names and un-prefixed names are checked against builtins.
	if prefix == "xsd" || prefix == "xs" || prefix == "" {
		if pi, ok := builtins[local]; ok {
			s := &jsonschema.Schema{Type: pi.typ}
			if pi.format != "" {
				s.Format = pi.format
			}
			return s
		}
	}
	// Anything else is a $ref to a defined type in $defs.
	return &jsonschema.Schema{Ref: "#/$defs/" + local}
}

// isRequiredElement reports whether an xs:element is required (minOccurs ≥ 1).
// The XSD default for minOccurs is 1.
func isRequiredElement(el *xsdElement) bool {
	if el.MinOccurs == "" || el.MinOccurs == "1" {
		return true
	}
	if n, err := strconv.Atoi(el.MinOccurs); err == nil {
		return n >= 1
	}
	return false
}

// maxOccursInt converts a maxOccurs string to an int; "unbounded" → -1.
func maxOccursInt(s string) int {
	if s == "unbounded" {
		return -1
	}
	if s == "" {
		return 1
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 1
	}
	return n
}

// localName strips the namespace prefix from a qualified name.
func localName(name string) string {
	if i := strings.LastIndex(name, ":"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// namePrefix returns the namespace prefix of a qualified name (empty if none).
func namePrefix(name string) string {
	if i := strings.LastIndex(name, ":"); i >= 0 {
		return name[:i]
	}
	return ""
}
