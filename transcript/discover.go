package transcript

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Session is one main-thread transcript and its subagent transcripts.
type Session struct {
	ID         string
	Path       string
	ProjectDir string
	Subagents  []Subagent
}

// Subagent is one subagent transcript belonging to a session.
type Subagent struct {
	AgentID string
	Path    string
}

// Discover finds every session under root (~/.claude/projects), sorted by
// project directory and then session id.
func Discover(root string) ([]Session, error) {
	projects, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Session
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		dir := filepath.Join(root, p.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			name := f.Name()
			if f.IsDir() || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			id := strings.TrimSuffix(name, ".jsonl")
			out = append(out, Session{
				ID:         id,
				Path:       filepath.Join(dir, name),
				ProjectDir: dir,
				Subagents:  discoverSubagents(filepath.Join(dir, id, "subagents")),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProjectDir != out[j].ProjectDir {
			return out[i].ProjectDir < out[j].ProjectDir
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func discoverSubagents(dir string) []Subagent {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Subagent
	for _, f := range files {
		name := f.Name()
		if f.IsDir() || !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		out = append(out, Subagent{
			AgentID: strings.TrimSuffix(strings.TrimPrefix(name, "agent-"), ".jsonl"),
			Path:    filepath.Join(dir, name),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID < out[j].AgentID })
	return out
}
