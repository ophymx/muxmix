package ffprobe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// PacketOrFrame is one element of the "packets_and_frames" array that
// ffprobe emits when -show_packets and -show_frames are combined. Exactly
// one of Packet or Frame is set; Type is ffprobe's own discriminator
// ("packet", "frame" or "subtitle").
type PacketOrFrame struct {
	Type   string
	Packet *Packet
	Frame  *Frame
}

// UnmarshalJSON dispatches on the "type" key ffprobe adds to mixed arrays.
func (pf *PacketOrFrame) UnmarshalJSON(data []byte) error {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	*pf = PacketOrFrame{Type: head.Type}
	switch head.Type {
	case "packet":
		pf.Packet = new(Packet)
		return json.Unmarshal(data, pf.Packet)
	case "frame", "subtitle":
		pf.Frame = new(Frame)
		return json.Unmarshal(data, pf.Frame)
	default:
		return fmt.Errorf("ffprobe: packets_and_frames entry has unknown type %q", head.Type)
	}
}

// MarshalJSON emits the wrapped value with its "type" discriminator first.
func (pf PacketOrFrame) MarshalJSON() ([]byte, error) {
	var body []byte
	var err error
	switch {
	case pf.Packet != nil:
		body, err = json.Marshal(pf.Packet)
	case pf.Frame != nil:
		body, err = json.Marshal(pf.Frame)
	default:
		return []byte("null"), nil
	}
	if err != nil {
		return nil, err
	}
	typ, _ := json.Marshal(pf.Type)
	if len(body) == 2 { // "{}"
		return []byte(`{"type":` + string(typ) + `}`), nil
	}
	return append([]byte(`{"type":`+string(typ)+`,`), body[1:]...), nil
}

// decodeExtra returns every top-level key of data that is not in known.
// Numbers are kept as json.Number so integers survive intact.
func decodeExtra(data []byte, known map[string]bool) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var all map[string]any
	if err := dec.Decode(&all); err != nil {
		return nil, err
	}
	for k := range all {
		if known[k] {
			delete(all, k)
		}
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all, nil
}

// encodeExtra marshals v (a struct without custom marshaling) and appends
// the extra keys in sorted order.
func encodeExtra(v any, extra map[string]any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return body, nil
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.Write(body[:len(body)-1]) // drop closing brace
	for _, k := range keys {
		if buf.Len() > 1 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, err := json.Marshal(extra[k])
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
