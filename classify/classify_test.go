package classify

import "testing"

func mustNew(t *testing.T, extra ...Rule) *Classifier {
	t.Helper()
	c, err := New(extra...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestClassifyBuiltins(t *testing.T) {
	c := mustNew(t)
	cases := []struct {
		cmd  string
		want Kind
	}{
		// Go
		{"go test ./...", KindTest},
		{"go test -run TestX -count=1 ./pkg", KindTest},
		{"go build ./cmd/wallclock", KindBuild},
		{"go install ./...", KindBuild},
		{"go run main.go", KindBuild},
		{"go vet ./...", KindLint},
		{"gofmt -l .", KindLint},
		{"golangci-lint run", KindLint},
		{"go mod tidy", KindPackage},
		{"go get github.com/x/y", KindPackage},
		{"go env GOPATH", KindBash},
		// Swift
		{"swift test", KindTest},
		{"swift test --filter FooTests", KindTest},
		{"swift build -c release", KindBuild},
		{"swift run mytool", KindBuild},
		{"xcodebuild test -scheme App -destination 'platform=macOS'", KindTest},
		{"xcodebuild -scheme App test", KindTest},
		{"xcodebuild -scheme App build", KindBuild},
		{"xcodebuild -scheme App", KindBuild},
		{"xcodebuild -scheme App clean build", KindBuild},
		{"swiftlint", KindLint},
		{"swiftlint --strict", KindLint},
		{"swift-format lint -r Sources", KindLint},
		{"swift package resolve", KindPackage},
		{"swift package update", KindPackage},
		{"swift package describe", KindBash},
		// Rust
		{"cargo test", KindTest},
		{"cargo test --workspace", KindTest},
		{"cargo nextest run", KindTest},
		{"cargo +nightly test", KindTest},
		{"cargo build --release", KindBuild},
		{"cargo run", KindBuild},
		{"cargo check", KindBuild},
		{"cargo clippy -- -D warnings", KindLint},
		{"cargo fmt --check", KindLint},
		{"cargo add serde", KindPackage},
		{"cargo update", KindPackage},
		{"cargo fetch", KindPackage},
		{"cargo doc", KindBash},
		// Python
		{"pytest", KindTest},
		{"pytest -x tests/", KindTest},
		{"python -m pytest tests", KindTest},
		{"python3 -m pytest", KindTest},
		{"python -m unittest discover", KindTest},
		{".venv/bin/pytest", KindTest},
		{"tox", KindTest},
		{"nox -s tests", KindTest},
		{"ruff check .", KindLint},
		{"black .", KindLint},
		{"mypy src", KindLint},
		{"flake8", KindLint},
		{"pip install -r requirements.txt", KindPackage},
		{"python -m pip install foo", KindPackage},
		{"uv pip install foo", KindPackage},
		{"uv sync", KindPackage},
		{"poetry install", KindPackage},
		{"python script.py", KindBash},
		// make and just
		{"make test", KindTest},
		{"make check", KindTest},
		{"make build", KindBuild},
		{"make all", KindBuild},
		{"make", KindBuild},
		{"make -j8", KindBuild},
		{"make -C sub test", KindTest},
		{"make clean", KindBash},
		{"just test", KindTest},
		{"just build", KindBuild},
		{"just", KindBuild},
		{"just deploy", KindBash},
		// JS
		{"npm test", KindTest},
		{"npm run test", KindTest},
		{"npm run test:unit", KindTest},
		{"npm run build", KindBuild},
		{"npm run lint", KindLint},
		{"npm install", KindPackage},
		{"npm ci", KindPackage},
		{"npm start", KindBash},
		{"pnpm test", KindTest},
		{"pnpm run build", KindBuild},
		{"pnpm install", KindPackage},
		{"yarn test", KindTest},
		{"yarn build", KindBuild},
		{"yarn", KindPackage},
		{"yarn install", KindPackage},
		{"yarn add left-pad", KindPackage},
		// VCS
		{"git status", KindVCS},
		{"git commit -m 'go test'", KindVCS},
		{"gh pr view 12", KindVCS},
		{"jj log", KindVCS},
		// Fallback
		{"ls -la", KindBash},
		{"", KindBash},
		{"   ", KindBash},
		{"cd /tmp", KindBash},
	}
	for _, tc := range cases {
		if got := c.Classify(tc.cmd); got != tc.want {
			t.Errorf("Classify(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

func TestClassifyPrefixesAndWrappers(t *testing.T) {
	c := mustNew(t)
	cases := []struct {
		cmd  string
		want Kind
	}{
		{"cd /repo && go test ./...", KindTest},
		{"cd /repo; go build", KindBuild},
		{"cd '/my repo' && swift test", KindTest},
		{"FOO=1 GOCACHE=x go test ./...", KindTest},
		{"GOFLAGS='-count=1 -v' go test", KindTest},
		{"CGO_ENABLED=0 go build", KindBuild},
		{"time go test", KindTest},
		{"time -p go build", KindBuild},
		{"nice -n 10 cargo build", KindBuild},
		{"env FOO=1 go test", KindTest},
		{"env -i PATH=/bin make test", KindTest},
		{"xcrun swift test", KindTest},
		{"xcrun --sdk macosx swift build", KindBuild},
		{"uv run pytest", KindTest},
		{"uv run python -m pytest -x", KindTest},
		{"poetry run pytest", KindTest},
		{"poetry run ruff check", KindLint},
		{"sudo -u me make build", KindBuild},
		{"timeout 60 go test", KindTest},
		{"/usr/bin/git status", KindVCS},
		{"(cd sub && go test)", KindTest},
		{"if go vet ./...; then echo ok; fi", KindLint},
		{"go test ./... 2>&1 | tail -20", KindTest},
		{"go test ./... > out.txt 2>&1", KindTest},
		{"go build && go test", KindTest},
		{"git pull && go build", KindBuild},
		{"go vet ./... && go build ./... && go test ./...", KindTest},
		{"go build || echo failed", KindBuild},
		{"gofmt -l . ; git status", KindLint},
		{"git add -A; go mod tidy", KindPackage},
		{"make build; make test", KindTest},
		{"go build ./... &", KindBuild},
		{"go test\\\n  ./...", KindTest},
		{"echo hi\ngo test", KindTest},
		{"# go test\nls", KindBash},
		{"x=$(git rev-parse HEAD) go build", KindBuild},
		{"npm --prefix web run build", KindBuild},
	}
	for _, tc := range cases {
		if got := c.Classify(tc.cmd); got != tc.want {
			t.Errorf("Classify(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

func TestClassifyIgnoresQuotedAndHeredoc(t *testing.T) {
	c := mustNew(t)
	cases := []struct {
		name string
		cmd  string
		want Kind
	}{
		{"double quoted", `echo "go test"`, KindBash},
		{"single quoted", `echo 'cargo build && make test'`, KindBash},
		{"quoted pipe", `echo "a | go test"`, KindBash},
		{"quoted semicolon", `grep 'x; go test' file`, KindBash},
		{"escaped quote", `echo "say \"go test\""`, KindBash},
		{"command substitution", `echo "$(go test)"`, KindBash},
		{"heredoc to cat", "cat <<EOF\ngo test ./...\nEOF", KindBash},
		{"heredoc to git", "git commit -F - <<EOF\nFix go test\ngo test ./...\nEOF", KindVCS},
		{"quoted heredoc delim", "git commit -F - <<'EOF'\ngo test\nEOF", KindVCS},
		{"dash heredoc", "cat <<-EOF\n\tgo test\n\tEOF", KindBash},
		{"command after heredoc", "cat <<EOF\nhello\nEOF\ngo test", KindTest},
		{"heredoc then pipe", "cat <<EOF | go test\nx\nEOF", KindTest},
		{"herestring", `cat <<< "go test"`, KindBash},
		{"redirect to file named test", `echo hi > go`, KindBash},
		{"comment", "ls # go test", KindBash},
		{"cd then quoted", `cd x && echo "make test"`, KindBash},
	}
	for _, tc := range cases {
		if got := c.Classify(tc.cmd); got != tc.want {
			t.Errorf("%s: Classify(%q) = %q, want %q", tc.name, tc.cmd, got, tc.want)
		}
	}
}

func TestKindValid(t *testing.T) {
	for _, k := range []Kind{KindTest, KindBuild, KindLint, KindPackage, KindVCS, KindBash} {
		if !k.Valid() {
			t.Errorf("%q should be valid", k)
		}
	}
	for _, k := range []Kind{"", "file", "TEST"} {
		if k.Valid() {
			t.Errorf("%q should be invalid", k)
		}
	}
}

// Regression: polling loops were the largest unclassified wait in real
// transcripts (2026-11-19).
func TestWaitKind(t *testing.T) {
	c := mustNew(t)
	cases := map[string]Kind{
		"sleep 30": KindWait,
		"until grep -q ready log.txt; do sleep 5; done":        KindWait,
		"for i in 1 2 3; do curl -s localhost; sleep 2; done":  KindWait,
		"until gh run view 123 | grep done; do sleep 30; done": KindWait,
		"gh run watch 123":                   KindWait,
		"gh pr checks 9 --watch":             KindWait,
		"sleep 5 && go test ./...":           KindTest,
		"node --test tests/":                 KindTest,
		"find . -name '*.go' | xargs go vet": KindLint,
		"git ls-files | xargs -n1 gofmt -l":  KindLint,
	}
	for cmd, want := range cases {
		if got := c.Classify(cmd); got != want {
			t.Errorf("Classify(%q) = %q, want %q", cmd, got, want)
		}
	}
}
