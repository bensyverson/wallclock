package repo

import (
	"os"
	"path/filepath"
	"strings"
)

// Lang is a repo's primary language, inferred from marker files.
type Lang string

const (
	LangGo      Lang = "go"
	LangSwift   Lang = "swift"
	LangRust    Lang = "rust"
	LangPython  Lang = "python"
	LangJS      Lang = "js"
	LangUnknown Lang = "unknown"
)

// langMarkers lists languages in priority order with their marker files.
var langMarkers = []struct {
	lang  Lang
	files []string
	exts  []string // directory-name suffixes, such as .xcodeproj
}{
	{LangGo, []string{"go.mod"}, nil},
	{LangSwift, []string{"Package.swift"}, []string{".xcodeproj", ".xcworkspace"}},
	{LangRust, []string{"Cargo.toml"}, nil},
	{LangPython, []string{"pyproject.toml", "setup.py", "requirements.txt"}, nil},
	{LangJS, []string{"package.json"}, nil},
}

// detectLanguages returns every language whose marker appears directly in
// root, in priority order.
func detectLanguages(root string) []Lang {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	var langs []Lang
	for _, m := range langMarkers {
		if hasMarker(root, names, m.files, m.exts) {
			langs = append(langs, m.lang)
		}
	}
	return langs
}

func hasMarker(root string, names map[string]bool, files, exts []string) bool {
	for _, f := range files {
		if names[f] {
			return true
		}
	}
	for n := range names {
		for _, ext := range exts {
			if strings.HasSuffix(n, ext) && isDir(filepath.Join(root, n)) {
				return true
			}
		}
	}
	return false
}
