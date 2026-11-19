package cli

import (
	"bytes"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Run(args, &out, &errb, Env{})
	return out.String(), errb.String(), code
}

func TestBareInvocationPrintsPrimer(t *testing.T) {
	out, _, code := run(t)
	if code != ExitOK {
		t.Fatalf("exit = %d, want %d", code, ExitOK)
	}
	for _, want := range []string{"wallclock", "report", "sessions", "cache", "wallclock report"} {
		if !strings.Contains(out, want) {
			t.Errorf("primer missing %q:\n%s", want, out)
		}
	}
}

func TestUnknownVerbIsUsageError(t *testing.T) {
	_, errOut, code := run(t, "frobnicate")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	for _, want := range []string{`"frobnicate"`, "report", "sessions", "cache"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
	}
}

func TestVerbHelpHasExample(t *testing.T) {
	for _, verb := range []string{"report", "sessions", "cache"} {
		out, _, code := run(t, verb, "--help")
		if code != ExitOK {
			t.Errorf("%s --help exit = %d", verb, code)
		}
		if !strings.Contains(out, "Example:") {
			t.Errorf("%s --help has no example:\n%s", verb, out)
		}
	}
}
