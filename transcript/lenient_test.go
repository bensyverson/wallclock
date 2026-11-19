package transcript

import "testing"

// A field of the wrong JSON type must not cost the whole entry.
func TestWronglyTypedFieldsKeepTheEntry(t *testing.T) {
	const ts = `"timestamp":"2026-11-13T19:00:00Z"`
	tests := []struct {
		name  string
		line  string
		check func(t *testing.T, e Entry)
	}{
		{"float thinkingDurationMs",
			`{"type":"assistant",` + ts + `,"requestRef":"r1","thinkingDurationMs":1234.5,"message":{"content":[]}}`,
			func(t *testing.T, e Entry) {
				if e.RequestRef != "r1" || e.ThinkingMs != 1234 {
					t.Errorf("got ref=%q thinking=%d", e.RequestRef, e.ThinkingMs)
				}
			}},
		{"float durationMs",
			`{"type":"system","subtype":"turn_duration",` + ts + `,"durationMs":1234.5}`,
			func(t *testing.T, e Entry) {
				if e.Subtype != SubtypeTurnDuration || e.DurationMs != 1234 {
					t.Errorf("got subtype=%q duration=%d", e.Subtype, e.DurationMs)
				}
			}},
		{"string origin",
			`{"type":"user",` + ts + `,"origin":"human","message":{"content":"hi"}}`,
			func(t *testing.T, e Entry) {
				if e.Origin != OriginHuman || e.Text != "hi" {
					t.Errorf("got origin=%q text=%q", e.Origin, e.Text)
				}
			}},
		{"string permissionDecision",
			`{"type":"user",` + ts + `,"permissionDecision":"allow","message":{"content":[]}}`,
			func(t *testing.T, e Entry) {
				if e.Permission.ByPerson() {
					t.Errorf("a bare decision string must not read as a person: %+v", e.Permission)
				}
			}},
		{"string isSidechain",
			`{"type":"user",` + ts + `,"isSidechain":"false","message":{"content":"hi"}}`,
			func(t *testing.T, e Entry) {
				if e.IsSidechain || e.Text != "hi" {
					t.Errorf("got sidechain=%v text=%q", e.IsSidechain, e.Text)
				}
			}},
		{"null numbers and objects",
			`{"type":"system","subtype":"turn_duration",` + ts + `,"durationMs":null,"origin":null,"permissionDecision":null}`,
			func(t *testing.T, e Entry) {
				if e.DurationMs != 0 || e.Permission != nil {
					t.Errorf("got %+v", e)
				}
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := parse(t, tc.line)
			if f.Malformed != 0 || len(f.Entries) != 1 {
				t.Fatalf("entries=%d malformed=%d", len(f.Entries), f.Malformed)
			}
			tc.check(t, f.Entries[0])
		})
	}
}

func TestBrokenJSONIsStillMalformed(t *testing.T) {
	f := parse(t, `{"type":"user",`)
	if f.Malformed != 1 || len(f.Entries) != 0 {
		t.Errorf("entries=%d malformed=%d", len(f.Entries), f.Malformed)
	}
}

func TestWellTypedFieldsStillDecode(t *testing.T) {
	f := parse(t, `{"type":"user","timestamp":"2026-11-13T19:00:00Z","isSidechain":true,"origin":{"kind":"peer"},"permissionDecision":{"decision":"accept","source":"user_temporary"},"message":{"content":"x"}}`)
	e := f.Entries[0]
	if !e.IsSidechain || e.Origin != OriginPeer || !e.Permission.ByPerson() {
		t.Errorf("got %+v", e)
	}
}
