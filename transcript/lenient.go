package transcript

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// The transcript format is Claude Code's internal log and drifts. These
// types decode a field of the expected shape and quietly yield the zero
// value for any other, so one mistyped field never costs the whole entry.

// lenientInt64 reads a JSON number as whole units, truncating a fraction.
type lenientInt64 int64

func (n *lenientInt64) UnmarshalJSON(b []byte) error {
	*n = 0
	var f float64
	if json.Unmarshal(b, &f) == nil {
		*n = lenientInt64(f)
	}
	return nil
}

// lenientBool reads a JSON bool, or the strings "true" and "false".
type lenientBool bool

func (v *lenientBool) UnmarshalJSON(b []byte) error {
	*v = false
	var x bool
	if json.Unmarshal(b, &x) == nil {
		*v = lenientBool(x)
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		if p, err := strconv.ParseBool(s); err == nil {
			*v = lenientBool(p)
		}
	}
	return nil
}

// rawOrigin reads origin as {"kind": …} or as a bare string.
type rawOrigin struct {
	Kind Origin
}

func (o *rawOrigin) UnmarshalJSON(b []byte) error {
	*o = rawOrigin{}
	var obj struct {
		Kind Origin `json:"kind"`
	}
	if json.Unmarshal(b, &obj) == nil {
		o.Kind = obj.Kind
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		o.Kind = Origin(s)
	}
	return nil
}

// rawPermission reads permissionDecision as an object, or as a bare
// decision string with no source.
type rawPermission struct {
	Permission
}

func (p *rawPermission) UnmarshalJSON(b []byte) error {
	*p = rawPermission{}
	if bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		var obj struct {
			Decision string `json:"decision"`
			Source   string `json:"source"`
		}
		if json.Unmarshal(b, &obj) == nil {
			p.Decision, p.Source = obj.Decision, obj.Source
		}
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		p.Decision = s
	}
	return nil
}
