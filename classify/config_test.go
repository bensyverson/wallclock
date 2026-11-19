package classify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtraRulesPrefix(t *testing.T) {
	c := mustNew(t, Rule{Kind: KindTest, Prefix: []string{"bazel", "test"}})
	if got := c.Classify("cd x && bazel test //..."); got != KindTest {
		t.Errorf("got %q, want test", got)
	}
	if got := c.Classify("bazel query //..."); got != KindBash {
		t.Errorf("got %q, want bash", got)
	}
}

func TestExtraRulesRegexp(t *testing.T) {
	c := mustNew(t, Rule{Kind: KindBuild, Regexp: `^bazel (build|run)\b`})
	if got := c.Classify("FOO=1 bazel run //app"); got != KindBuild {
		t.Errorf("got %q, want build", got)
	}
	if got := c.Classify("bazel clean"); got != KindBash {
		t.Errorf("got %q, want bash", got)
	}
}

func TestExtraRulesOverrideBuiltins(t *testing.T) {
	c := mustNew(t, Rule{Kind: KindLint, Prefix: []string{"go", "vet"}}, Rule{Kind: KindBuild, Prefix: []string{"make", "docs"}})
	if got := c.Classify("make docs"); got != KindBuild {
		t.Errorf("got %q, want build", got)
	}
	if got := c.Classify("go test"); got != KindTest {
		t.Errorf("builtins must still work, got %q", got)
	}
}

func TestExtraRulesDoNotMatchQuoted(t *testing.T) {
	c := mustNew(t, Rule{Kind: KindTest, Prefix: []string{"bazel", "test"}})
	if got := c.Classify(`echo "bazel test"`); got != KindBash {
		t.Errorf("got %q, want bash", got)
	}
}

func TestNewRejectsBadRules(t *testing.T) {
	bad := map[string]Rule{
		"bad kind":      {Kind: "nope", Prefix: []string{"x"}},
		"empty kind":    {Prefix: []string{"x"}},
		"neither":       {Kind: KindTest},
		"both":          {Kind: KindTest, Prefix: []string{"x"}, Regexp: "x"},
		"bad regexp":    {Kind: KindTest, Regexp: "("},
		"empty prefix":  {Kind: KindTest, Prefix: []string{}},
		"blank element": {Kind: KindTest, Prefix: []string{""}},
	}
	for name, r := range bad {
		if _, err := New(r); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestLoadRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	body := `{"rules":[
		{"kind":"test","prefix":["bazel","test"]},
		{"kind":"build","regexp":"^bazel build"}
	]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rules, err := LoadRules(path)
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	if len(rules) != 2 || rules[0].Kind != KindTest || rules[1].Regexp != "^bazel build" {
		t.Fatalf("unexpected rules: %+v", rules)
	}
	c := mustNew(t, rules...)
	if got := c.Classify("bazel build //x"); got != KindBuild {
		t.Errorf("got %q", got)
	}
}

func TestLoadRulesErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadRules(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("missing file should error")
	}
	for name, body := range map[string]string{
		"malformed":     `{"rules": [`,
		"unknown field": `{"rules":[{"kind":"test","prefix":["a"],"extra":1}]}`,
		"bad kind":      `{"rules":[{"kind":"zzz","prefix":["a"]}]}`,
	} {
		p := filepath.Join(dir, name+".json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadRules(p); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
