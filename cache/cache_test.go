package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bensyverson/wallclock/transcript"
)

const line = `{"type":"api-request","id":"r1","timestamp":"2026-11-13T19:53:11.484Z"}` + "\n"

func setup(t *testing.T) (c *Cache, src string, parses *int) {
	t.Helper()
	dir := t.TempDir()
	src = filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(src, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	n := 0
	c = Open(filepath.Join(dir, "cache"))
	c.parse = func(p string) (*transcript.File, error) {
		n++
		return transcript.ParseFile(p)
	}
	return c, src, &n
}

func load(t *testing.T, c *Cache, src string) *transcript.File {
	t.Helper()
	f, err := c.Load(src)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return f
}

func TestHitOnUnchangedFile(t *testing.T) {
	c, src, parses := setup(t)
	first := load(t, c, src)
	second := load(t, Reopen(c), src)
	if *parses != 1 {
		t.Errorf("parses = %d, want 1", *parses)
	}
	if len(second.Entries) != 1 || second.Entries[0].RequestID != "r1" || !second.Entries[0].Timestamp.Equal(first.Entries[0].Timestamp) {
		t.Errorf("cached entries = %+v", second.Entries)
	}
}

func TestMissAfterChange(t *testing.T) {
	c, src, parses := setup(t)
	load(t, c, src)
	if err := os.WriteFile(src, []byte(line+line), 0o644); err != nil {
		t.Fatal(err)
	}
	f := load(t, Reopen(c), src)
	if *parses != 2 || len(f.Entries) != 2 {
		t.Errorf("parses = %d entries = %d, want 2 and 2", *parses, len(f.Entries))
	}
}

func TestMissAfterMtimeChange(t *testing.T) {
	c, src, parses := setup(t)
	load(t, c, src)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(src, later, later); err != nil {
		t.Fatal(err)
	}
	load(t, Reopen(c), src)
	if *parses != 2 {
		t.Errorf("parses = %d, want 2", *parses)
	}
}

func TestMissAfterSchemaBump(t *testing.T) {
	c, src, parses := setup(t)
	load(t, c, src)
	c2 := Reopen(c)
	c2.version = c.version + 1
	load(t, c2, src)
	if *parses != 2 {
		t.Errorf("parses = %d, want 2", *parses)
	}
}

func TestDisabledCacheAlwaysParses(t *testing.T) {
	_, src, _ := setup(t)
	c := Open("")
	n := 0
	c.parse = func(p string) (*transcript.File, error) { n++; return transcript.ParseFile(p) }
	load(t, c, src)
	load(t, c, src)
	if n != 2 {
		t.Errorf("parses = %d, want 2", n)
	}
}

func TestClearAndStats(t *testing.T) {
	c, src, _ := setup(t)
	load(t, c, src)
	files, size, err := c.Stats()
	if err != nil || files != 1 || size == 0 {
		t.Errorf("stats = %d, %d, %v", files, size, err)
	}
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	if files, _, _ := c.Stats(); files != 0 {
		t.Errorf("files after clear = %d", files)
	}
}

func TestMissingSourceIsError(t *testing.T) {
	c, _, _ := setup(t)
	if _, err := c.Load("/nonexistent/x.jsonl"); err == nil {
		t.Error("want error for missing source")
	}
}

// Regression (review 2026-11-19): clear deleted any dir starting with "v".
func TestClearKeepsForeignDirs(t *testing.T) {
	c, src, _ := setup(t)
	load(t, c, src)
	for _, d := range []string{"venv", "vendor", "v2x"} {
		if err := os.MkdirAll(filepath.Join(c.Dir(), d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"venv", "vendor", "v2x"} {
		if _, err := os.Stat(filepath.Join(c.Dir(), d)); err != nil {
			t.Errorf("%s removed: %v", d, err)
		}
	}
}

// Regression (review 2026-11-19): a record with a null file was a hit.
func TestNullFileRecordIsMiss(t *testing.T) {
	c, src, parses := setup(t)
	load(t, c, src)
	key := c.keyPath(src)
	data, _ := os.ReadFile(key)
	data = []byte(strings.Replace(string(data), `"file":{`, `"file":null,"x":{`, 1))
	if err := os.WriteFile(key, data, 0o644); err != nil {
		t.Fatal(err)
	}
	f := load(t, Reopen(c), src)
	if f == nil || *parses != 2 {
		t.Errorf("file = %v, parses = %d; want a re-parse", f, *parses)
	}
}
