// gen-schema converts the ffprobe XSD to a JSON Schema (draft-07) that
// describes ffprobe's JSON output format (as produced by
// `ffprobe -print_format json -show_format -show_streams`).
//
// In the default form, the tool reads a sibling ffprobe.version file,
// derives the cached XSD filename (for example ffprobe-n7.1.3.xsd), and
// downloads it when missing. The XSD describes the XML structure; this tool
// adapts the generated schema for the actual JSON output by:
//   - Replacing tagsType with an additionalProperties string map.
//   - Inlining single-element wrapper types (streamsType → direct array).
//   - Replacing choice-based wrappers (framesType) with typed arrays.
//   - Changing xsd:float fields that ffprobe outputs as strings to {type:string}.
//   - Changing xsd:boolean fields that ffprobe outputs as 0/1 to {type:integer}.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/ophymx/muxmix/xsd2jsonschema"
)

const (
	targetVersionFile     = "ffprobe.version"
	ffprobeXSDURLTemplate = "https://raw.githubusercontent.com/FFmpeg/FFmpeg/refs/tags/%s/doc/ffprobe.xsd"
)

func main() {
	var (
		xsdPath string
		outPath string
		err     error
	)

	switch len(os.Args) {
	case 2:
		outPath = os.Args[1]
		xsdPath, err = defaultXSDPath(filepath.Dir(outPath))
	case 3:
		xsdPath = os.Args[1]
		outPath = os.Args[2]
	default:
		fmt.Fprintln(os.Stderr, "usage: gen-schema [<input.xsd>] <output.schema.json>")
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-schema: %v\n", err)
		os.Exit(1)
	}

	if err := ensureXSD(xsdPath); err != nil {
		fmt.Fprintf(os.Stderr, "gen-schema: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Open(xsdPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-schema: open %s: %v\n", xsdPath, err)
		os.Exit(1)
	}
	defer f.Close()

	schema, err := xsd2jsonschema.Convert(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-schema: convert: %v\n", err)
		os.Exit(1)
	}

	postProcess(schema)

	out, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-schema: create %s: %v\n", outPath, err)
		os.Exit(1)
	}
	defer out.Close()

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(schema); err != nil {
		fmt.Fprintf(os.Stderr, "gen-schema: encode: %v\n", err)
		os.Exit(1)
	}
}

func ensureXSD(xsdPath string) error {
	if _, err := os.Stat(xsdPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", xsdPath, err)
	}

	version, err := readTargetVersion(filepath.Dir(xsdPath))
	if err != nil {
		return err
	}
	url := fmt.Sprintf(ffprobeXSDURLTemplate, version)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected status %s", url, resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(xsdPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(xsdPath), err)
	}
	tmpPath := xsdPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmpPath, err)
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, xsdPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename %s to %s: %w", tmpPath, xsdPath, err)
	}
	return nil
}

func defaultXSDPath(dir string) (string, error) {
	version, err := readTargetVersion(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("ffprobe-%s.xsd", version)), nil
}

func readTargetVersion(dir string) (string, error) {
	path := filepath.Join(dir, targetVersionFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	version := strings.TrimSpace(string(b))
	if version == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return version, nil
}

// postProcess adapts the XSD-derived schema to match ffprobe's JSON output format.
func postProcess(s *jsonschema.Schema) {
	if s.Defs == nil {
		return
	}

	// 1. Replace TagsType with a string→string map (JSON "tags" is always
	//    a flat {key: value} object, not an array of tag elements).
	if tagsName, ok := findDefName(s, "tagsType", "TagsType"); ok {
		s.Defs[tagsName] = &jsonschema.Schema{
			Type:                 "object",
			AdditionalProperties: &jsonschema.Schema{Type: "string"},
		}
	}

	// 2. Inline single-property array wrappers in FfprobeType.
	inlineSingleArrayWrappers(s)

	// 3. Inline choice-based wrappers (frame/subtitle mixed arrays).
	inlineChoiceWrappers(s)

	// 4. Fix XSD types that don't match ffprobe's actual JSON output.
	fixJSONTypes(s)
}

// inlineSingleArrayWrappers replaces $ref entries in FfprobeType that point to
// a wrapper type that has exactly one property (an array) with the array schema
// directly.  This ensures that e.g. "streams" becomes an array, not an object
// with a "stream" property.
func inlineSingleArrayWrappers(s *jsonschema.Schema) {
	root := findRootObject(s)
	if root == nil || root.Properties == nil {
		return
	}
	for propName, propSchema := range root.Properties {
		if propSchema.Ref == "" {
			continue
		}
		defName := lastSegment(propSchema.Ref)
		def, ok := s.Defs[defName]
		if !ok || len(def.Properties) != 1 {
			continue
		}
		for _, inner := range def.Properties {
			if inner.Type == "array" || inner.Items != nil {
				root.Properties[propName] = inner
			}
			break
		}
	}
}

// inlineChoiceWrappers handles wrapper types derived from xs:choice where
// multiple element types can appear (framesType, packetsAndFramesType).
// It replaces the $ref in FfprobeType with a direct array whose items use anyOf.
func inlineChoiceWrappers(s *jsonschema.Schema) {
	root := findRootObject(s)
	if root == nil || root.Properties == nil {
		return
	}
	// Map wrapper def name → contained item type names.
	choiceTypes := map[string][]string{
		"framesType":           {"frameType", "subtitleType"},
		"packetsAndFramesType": {"packetType", "frameType", "subtitleType"},
	}
	for propName, propSchema := range root.Properties {
		if propSchema.Ref == "" {
			continue
		}
		defName := lastSegment(propSchema.Ref)
		itemTypes, ok := choiceTypes[defName]
		if !ok {
			continue
		}
		anyOf := make([]*jsonschema.Schema, len(itemTypes))
		for i, t := range itemTypes {
			anyOf[i] = &jsonschema.Schema{Ref: "#/$defs/" + t}
		}
		root.Properties[propName] = &jsonschema.Schema{
			Type:  "array",
			Items: &jsonschema.Schema{AnyOf: anyOf},
		}
	}
}

// fixJSONTypes adjusts field types to match what ffprobe actually outputs in
// JSON mode.  Common mismatches:
//
//   - xsd:float / xsd:long fields like "duration", "bit_rate", "size" are
//     always emitted as JSON strings by ffprobe.
//   - xsd:boolean fields "closed_captions" and "film_grain" are emitted as
//     integer 0/1, not JSON true/false.
func fixJSONTypes(s *jsonschema.Schema) {
	strType := func() *jsonschema.Schema { return &jsonschema.Schema{Type: "string"} }
	intType := func() *jsonschema.Schema { return &jsonschema.Schema{Type: "integer"} }

	// Stream fields that ffprobe JSON outputs as strings.
	streamStrings := []string{
		"start_time", "duration",
		"bit_rate", "max_bit_rate",
		"bits_per_raw_sample",
		"nb_frames", "nb_read_frames", "nb_read_packets",
		"sample_rate",
		"pts_time", "dts_time", "duration_time",
	}
	// Stream xsd:boolean fields emitted as 0/1 integers.
	streamBools := []string{"closed_captions", "film_grain"}

	if streamName, ok := findDefName(s, "streamType", "StreamType"); ok {
		st := s.Defs[streamName]
		for _, name := range streamStrings {
			if _, ok := st.Properties[name]; ok {
				st.Properties[name] = strType()
			}
		}
		for _, name := range streamBools {
			if _, ok := st.Properties[name]; ok {
				st.Properties[name] = intType()
			}
		}
	}

	// Format fields that ffprobe JSON outputs as strings.
	fmtStrings := []string{"start_time", "duration", "size", "bit_rate"}
	if formatName, ok := findDefName(s, "formatType", "FormatType"); ok {
		ft := s.Defs[formatName]
		for _, name := range fmtStrings {
			if _, ok := ft.Properties[name]; ok {
				ft.Properties[name] = strType()
			}
		}
	}

	// Packet type numeric-as-string fields.
	packetStrings := []string{
		"pts_time", "dts_time", "duration_time",
	}
	if packetName, ok := findDefName(s, "packetType", "PacketType"); ok {
		pt := s.Defs[packetName]
		for _, name := range packetStrings {
			if _, ok := pt.Properties[name]; ok {
				pt.Properties[name] = strType()
			}
		}
	}
}

// findRootObject resolves the schema pointed to by the root $ref.
func findRootObject(s *jsonschema.Schema) *jsonschema.Schema {
	if s == nil || s.Defs == nil {
		return nil
	}
	if s.Ref != "" {
		if name := lastSegment(s.Ref); name != "" {
			if root, ok := s.Defs[name]; ok {
				return root
			}
		}
	}
	if name, ok := findDefName(s, "ffprobeType", "FfprobeType"); ok {
		return s.Defs[name]
	}
	return nil
}

// findDefName returns the first existing definition name from candidates,
// falling back to case-insensitive matching.
func findDefName(s *jsonschema.Schema, candidates ...string) (string, bool) {
	if s == nil || s.Defs == nil {
		return "", false
	}
	for _, c := range candidates {
		if _, ok := s.Defs[c]; ok {
			return c, true
		}
	}
	lower := map[string]string{}
	keys := make([]string, 0, len(s.Defs))
	for k := range s.Defs {
		keys = append(keys, k)
		lower[strings.ToLower(k)] = k
	}
	sort.Strings(keys)
	for _, c := range candidates {
		if k, ok := lower[strings.ToLower(c)]; ok {
			return k, true
		}
	}
	return "", false
}

// lastSegment returns the portion after the last "/" in a $ref like "#/$defs/Foo".
func lastSegment(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}
