# XSD to JSON Schema Converter

A Go package that converts XML Schema Definition (XSD) files to JSON Schema (Draft-07) format.

## Overview

This package provides functionality to parse XSD files and convert them into equivalent JSON Schema representations. It supports the conversion of various XSD constructs including primitive types, complex types, simple types, attributes, sequences, choices, and inheritance patterns.

## Features

- **Comprehensive Type Mapping**: Converts XSD primitive types to appropriate JSON Schema types
- **Complex Type Support**: Handles `xs:complexType` with sequences, choices, and all particles
- **Inheritance Support**: Supports `xs:complexContent` and `xs:simpleContent` for type extension
- **Attribute Handling**: Processes attributes with proper required/optional semantics
- **JSON Schema Draft-07**: Generates valid JSON Schema Draft-07 output
- **Reference Resolution**: Properly handles type references and generates `$defs` definitions

## Supported XSD Features

### Primitive Types
- **Strings**: `string`, `normalizedString`, `token`, `language`, `Name`, `NCName`, `ID`, `IDREF`, `anyURI`, etc.
- **Date/Time**: `dateTime`, `date`, `time`, `duration`, `gYear`, `gYearMonth`, etc.
- **Boolean**: `boolean`
- **Integers**: `int`, `integer`, `long`, `short`, `byte`, `unsignedInt`, `positiveInteger`, etc.
- **Numbers**: `decimal`, `float`, `double`
- **Binary**: `hexBinary`, `base64Binary`

### Complex Constructs
- **Complex Types**: `xs:complexType` with name and inline definitions
- **Simple Types**: `xs:simpleType` definitions
- **Elements**: `xs:element` with type references and inline types
- **Attributes**: `xs:attribute` with required/optional usage
- **Particles**: `xs:sequence`, `xs:choice`, `xs:all` with cardinality support
- **Content Models**: `xs:complexContent` and `xs:simpleContent` for inheritance

## Usage

```go
package main

import (
    "os"
    "fmt"
    "encoding/json"
    
    "github.com/ophymx/muxmix/xsd2jsonschema"
)

func main() {
    // Open XSD file
    file, err := os.Open("schema.xsd")
    if err != nil {
        panic(err)
    }
    defer file.Close()
    
    // Convert to JSON Schema
    schema, err := xsd2jsonschema.Convert(file)
    if err != nil {
        panic(err)
    }
    
    // Output as JSON
    output, _ := json.MarshalIndent(schema, "", "  ")
    fmt.Println(string(output))
}
```

## Example Conversion

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="http://example.com/test"
           xmlns:t="http://example.com/test">
    
    <xs:element name="person" type="t:personType"/>
    
    <xs:complexType name="personType">
        <xs:sequence>
            <xs:element name="name" type="xs:string"/>
            <xs:element name="age" type="xs:int"/>
        </xs:sequence>
        <xs:attribute name="id" type="xs:string" use="required"/>
    </xs:complexType>
</xs:schema>
```

**Generated JSON Schema:**
```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "$ref": "#/$defs/personType",
  "$defs": {
    "personType": {
      "type": "object",
      "properties": {
        "id": {
          "type": "string"
        },
        "name": {
          "type": "string"
        },
        "age": {
          "type": "integer"
        }
      },
      "required": ["id", "name", "age"]
    }
  }
}
```

## API Reference

### Convert Function

```go
func Convert(r io.Reader) (*jsonschema.Schema, error)
```

Parses an XSD from the provided reader and returns the equivalent JSON Schema (Draft-07).

**Parameters:**
- `r`: An `io.Reader` containing the XSD content

**Returns:**
- `*jsonschema.Schema`: The converted JSON Schema object
- `error`: Any error encountered during conversion

## Dependencies

- `github.com/google/jsonschema-go/jsonschema`: JSON Schema library for Go
- Standard library packages: `encoding/xml`, `io`, `fmt`, etc.

## Testing

The package includes comprehensive test coverage with examples of various XSD constructs:

```bash
go test ./...
```

## Limitations

- Currently supports JSON Schema Draft-07
- Some advanced XSD features may not be fully supported
- Complex inheritance patterns may require additional testing

## License

This package is part of the muxmix project. Please refer to the repository's main license file for licensing information.