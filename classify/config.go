package classify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

// Rule adds a pattern to a Classifier. Exactly one of Prefix and Regexp is
// set. Prefix matches the leading words of a command after launchers and env
// assignments are removed; Regexp matches those words joined
// by single spaces.
type Rule struct {
	Kind   Kind     `json:"kind"`
	Prefix []string `json:"prefix,omitempty"`
	Regexp string   `json:"regexp,omitempty"`
}

type compiledRule struct {
	kind   Kind
	prefix []string
	re     *regexp.Regexp
}

func (r Rule) compile() (compiledRule, error) {
	if !r.Kind.Valid() {
		return compiledRule{}, fmt.Errorf("classify: rule has invalid kind %q", r.Kind)
	}
	hasPrefix, hasRegexp := r.Prefix != nil, r.Regexp != ""
	switch {
	case hasPrefix == hasRegexp:
		return compiledRule{}, errors.New("classify: rule needs exactly one of prefix and regexp")
	case hasPrefix:
		if len(r.Prefix) == 0 {
			return compiledRule{}, errors.New("classify: rule prefix is empty")
		}
		if slices.Contains(r.Prefix, "") {
			return compiledRule{}, errors.New("classify: rule prefix has an empty word")
		}
		return compiledRule{kind: r.Kind, prefix: r.Prefix}, nil
	}
	re, err := regexp.Compile(r.Regexp)
	if err != nil {
		return compiledRule{}, fmt.Errorf("classify: rule regexp: %w", err)
	}
	return compiledRule{kind: r.Kind, re: re}, nil
}

func (r compiledRule) matches(toks []string) bool {
	if r.re != nil {
		return r.re.MatchString(strings.Join(toks, " "))
	}
	return hasPrefix(toks, r.prefix)
}

type rulesFile struct {
	Rules []Rule `json:"rules"`
}

// LoadRules reads extra rules from a JSON file of the form
// {"rules": [{"kind": "test", "prefix": ["bazel", "test"]},
// {"kind": "build", "regexp": "^bazel build"}]}.
func LoadRules(path string) ([]Rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("classify: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f rulesFile
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("classify: parsing %s: %w", path, err)
	}
	for _, r := range f.Rules {
		if _, err := r.compile(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return f.Rules, nil
}
