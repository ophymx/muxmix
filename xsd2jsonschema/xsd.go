package xsd2jsonschema

import "encoding/xml"

// xsdSchema is the top-level xs:schema element.
type xsdSchema struct {
	XMLName      xml.Name          `xml:"http://www.w3.org/2001/XMLSchema schema"`
	TargetNS     string            `xml:"targetNamespace,attr"`
	Elements     []*xsdElement     `xml:"http://www.w3.org/2001/XMLSchema element"`
	ComplexTypes []*xsdComplexType `xml:"http://www.w3.org/2001/XMLSchema complexType"`
	SimpleTypes  []*xsdSimpleType  `xml:"http://www.w3.org/2001/XMLSchema simpleType"`
}

// xsdElement is xs:element.
type xsdElement struct {
	XMLName     xml.Name        `xml:"http://www.w3.org/2001/XMLSchema element"`
	Name        string          `xml:"name,attr"`
	Type        string          `xml:"type,attr"`
	Ref         string          `xml:"ref,attr"`
	MinOccurs   string          `xml:"minOccurs,attr"`
	MaxOccurs   string          `xml:"maxOccurs,attr"`
	ComplexType *xsdComplexType `xml:"http://www.w3.org/2001/XMLSchema complexType"`
	SimpleType  *xsdSimpleType  `xml:"http://www.w3.org/2001/XMLSchema simpleType"`
}

// xsdComplexType is xs:complexType.
type xsdComplexType struct {
	XMLName        xml.Name           `xml:"http://www.w3.org/2001/XMLSchema complexType"`
	Name           string             `xml:"name,attr"`
	Sequence       *xsdParticle       `xml:"http://www.w3.org/2001/XMLSchema sequence"`
	Choice         *xsdParticle       `xml:"http://www.w3.org/2001/XMLSchema choice"`
	All            *xsdParticle       `xml:"http://www.w3.org/2001/XMLSchema all"`
	Attributes     []*xsdAttribute    `xml:"http://www.w3.org/2001/XMLSchema attribute"`
	ComplexContent *xsdComplexContent `xml:"http://www.w3.org/2001/XMLSchema complexContent"`
	SimpleContent  *xsdSimpleContent  `xml:"http://www.w3.org/2001/XMLSchema simpleContent"`
}

// xsdParticle represents xs:sequence, xs:choice, or xs:all.
type xsdParticle struct {
	MinOccurs string         `xml:"minOccurs,attr"`
	MaxOccurs string         `xml:"maxOccurs,attr"`
	Elements  []*xsdElement  `xml:"http://www.w3.org/2001/XMLSchema element"`
	Sequences []*xsdParticle `xml:"http://www.w3.org/2001/XMLSchema sequence"`
	Choices   []*xsdParticle `xml:"http://www.w3.org/2001/XMLSchema choice"`
}

// xsdAttribute is xs:attribute (a schema-level child, not an XML attribute).
type xsdAttribute struct {
	XMLName xml.Name `xml:"http://www.w3.org/2001/XMLSchema attribute"`
	Name    string   `xml:"name,attr"`
	Type    string   `xml:"type,attr"`
	Use     string   `xml:"use,attr"` // "required", "optional", "prohibited"
	Default string   `xml:"default,attr"`
	Fixed   string   `xml:"fixed,attr"`
}

// xsdSimpleType is xs:simpleType.
type xsdSimpleType struct {
	XMLName     xml.Name        `xml:"http://www.w3.org/2001/XMLSchema simpleType"`
	Name        string          `xml:"name,attr"`
	Restriction *xsdRestriction `xml:"http://www.w3.org/2001/XMLSchema restriction"`
	Union       *xsdUnion       `xml:"http://www.w3.org/2001/XMLSchema union"`
	List        *xsdList        `xml:"http://www.w3.org/2001/XMLSchema list"`
}

// xsdRestriction is xs:restriction.
type xsdRestriction struct {
	Base           string            `xml:"base,attr"`
	Enumerations   []*xsdEnumeration `xml:"http://www.w3.org/2001/XMLSchema enumeration"`
	MinLength      *xsdFacet         `xml:"http://www.w3.org/2001/XMLSchema minLength"`
	MaxLength      *xsdFacet         `xml:"http://www.w3.org/2001/XMLSchema maxLength"`
	Length         *xsdFacet         `xml:"http://www.w3.org/2001/XMLSchema length"`
	Pattern        *xsdFacet         `xml:"http://www.w3.org/2001/XMLSchema pattern"`
	MinInclusive   *xsdFacet         `xml:"http://www.w3.org/2001/XMLSchema minInclusive"`
	MaxInclusive   *xsdFacet         `xml:"http://www.w3.org/2001/XMLSchema maxInclusive"`
	MinExclusive   *xsdFacet         `xml:"http://www.w3.org/2001/XMLSchema minExclusive"`
	MaxExclusive   *xsdFacet         `xml:"http://www.w3.org/2001/XMLSchema maxExclusive"`
	TotalDigits    *xsdFacet         `xml:"http://www.w3.org/2001/XMLSchema totalDigits"`
	FractionDigits *xsdFacet         `xml:"http://www.w3.org/2001/XMLSchema fractionDigits"`
}

// xsdFacet is a single-value constraining facet.
type xsdFacet struct {
	Value string `xml:"value,attr"`
}

// xsdEnumeration is xs:enumeration.
type xsdEnumeration struct {
	Value string `xml:"value,attr"`
}

// xsdUnion is xs:union.
type xsdUnion struct {
	MemberTypes string `xml:"memberTypes,attr"`
}

// xsdList is xs:list.
type xsdList struct {
	ItemType string `xml:"itemType,attr"`
}

// xsdComplexContent is xs:complexContent.
type xsdComplexContent struct {
	Extension   *xsdCCExtension   `xml:"http://www.w3.org/2001/XMLSchema extension"`
	Restriction *xsdCCRestriction `xml:"http://www.w3.org/2001/XMLSchema restriction"`
}

// xsdCCExtension is xs:extension inside xs:complexContent.
type xsdCCExtension struct {
	Base       string          `xml:"base,attr"`
	Sequence   *xsdParticle    `xml:"http://www.w3.org/2001/XMLSchema sequence"`
	Choice     *xsdParticle    `xml:"http://www.w3.org/2001/XMLSchema choice"`
	Attributes []*xsdAttribute `xml:"http://www.w3.org/2001/XMLSchema attribute"`
}

// xsdCCRestriction is xs:restriction inside xs:complexContent.
type xsdCCRestriction struct {
	Base       string          `xml:"base,attr"`
	Sequence   *xsdParticle    `xml:"http://www.w3.org/2001/XMLSchema sequence"`
	Attributes []*xsdAttribute `xml:"http://www.w3.org/2001/XMLSchema attribute"`
}

// xsdSimpleContent is xs:simpleContent.
type xsdSimpleContent struct {
	Extension *xsdSCExtension `xml:"http://www.w3.org/2001/XMLSchema extension"`
}

// xsdSCExtension is xs:extension inside xs:simpleContent.
type xsdSCExtension struct {
	Base       string          `xml:"base,attr"`
	Attributes []*xsdAttribute `xml:"http://www.w3.org/2001/XMLSchema attribute"`
}
