package classify

import (
	"path"
	"regexp"
	"strings"
)

// Classifier assigns a Kind to shell command lines. Rules supplied to New
// are tried before the built-in patterns. A Classifier is safe for
// concurrent use.
type Classifier struct {
	extra []compiledRule
}

// New returns a Classifier with the built-in patterns plus extra rules.
func New(extra ...Rule) (*Classifier, error) {
	c := &Classifier{}
	for _, r := range extra {
		cr, err := r.compile()
		if err != nil {
			return nil, err
		}
		c.extra = append(c.extra, cr)
	}
	return c, nil
}

// Classify returns the Kind of a command line. Each segment of a chain or
// pipeline is classified on its own and the highest-priority kind wins:
// test, build, lint, package, vcs, then bash.
func (c *Classifier) Classify(cmd string) Kind {
	best := KindBash
	for _, seg := range splitSegments(cmd) {
		if k := c.classifySegment(seg); priority[k] > priority[best] {
			best = k
		}
	}
	return best
}

func (c *Classifier) classifySegment(seg []string) Kind {
	toks := unwrap(seg)
	if len(toks) == 0 {
		return KindBash
	}
	for _, r := range c.extra {
		if r.matches(toks) {
			return r.kind
		}
	}
	return builtin(toks)
}

var (
	pythonName = regexp.MustCompile(`^python(3(\.\d+)?)?$`)
	assignWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\+?=`)
)

// shell keywords that can precede a command.
var keywords = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "do": true,
	"while": true, "until": true, "!": true, "{": true,
}

// wrappers maps a launcher command to its flags that take a separate value.
var wrappers = map[string][]string{
	"time":    nil,
	"command": nil,
	"exec":    nil,
	"nohup":   nil,
	"builtin": nil,
	"sudo":    {"-u", "-g", "-h", "-p"},
	"nice":    {"-n"},
	"env":     {"-u", "-C", "-S"},
	"xcrun":   {"--sdk", "-sdk", "--toolchain", "-toolchain"},
}

// subcommandWrappers launch the rest of the line after "<name> run".
var subcommandWrappers = map[string][]string{
	"uv":     {"--with", "--python", "-p", "--group", "--extra", "--project", "--directory", "--env-file"},
	"poetry": {"-C", "--directory"},
}

// unwrap strips env assignments, shell keywords and launchers from a segment
// and returns the command that actually runs, with its name reduced to a base
// name and `python -m mod` rewritten to `mod`.
func unwrap(t []string) []string {
	for len(t) > 0 {
		w := t[0]
		if assignWord.MatchString(w) || keywords[w] {
			t = t[1:]
			continue
		}
		name := path.Base(w)
		if flags, ok := wrappers[name]; ok {
			t = skipFlags(t[1:], flags)
			continue
		}
		if name == "timeout" {
			t = skipFlags(t[1:], []string{"-s", "-k"})
			if len(t) > 0 {
				t = t[1:] // the duration
			}
			continue
		}
		if flags, ok := subcommandWrappers[name]; ok && len(t) > 1 && t[1] == "run" {
			t = skipFlags(t[2:], flags)
			continue
		}
		return normalize(t)
	}
	return nil
}

// skipFlags drops leading flags; those in withValue also consume the next word.
func skipFlags(t []string, withValue []string) []string {
	for len(t) > 0 && len(t[0]) > 1 && t[0][0] == '-' {
		if t[0] == "--" {
			return t[1:]
		}
		skip := 1
		for _, f := range withValue {
			if t[0] == f {
				skip = 2
			}
		}
		if skip > len(t) {
			skip = len(t)
		}
		t = t[skip:]
	}
	return t
}

func normalize(t []string) []string {
	name := path.Base(t[0])
	if pythonName.MatchString(name) && len(t) > 2 && t[1] == "-m" {
		return append([]string(nil), t[2:]...)
	}
	out := append([]string{name}, t[1:]...)
	if name == "cargo" && len(out) > 1 && strings.HasPrefix(out[1], "+") {
		out = append(out[:1], out[2:]...)
	}
	return out
}
