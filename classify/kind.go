// Package classify maps a shell command line to a Kind so that time spent
// waiting on Bash calls can be grouped (tests, builds, lint, and so on).
package classify

// Kind is the type of a Bash command.
type Kind string

const (
	KindTest    Kind = "test"
	KindBuild   Kind = "build"
	KindLint    Kind = "lint"
	KindPackage Kind = "package"
	KindVCS     Kind = "vcs"
	// KindWait is sleeping or polling: waiting on something outside the
	// command, such as CI, a deploy or a background process.
	KindWait Kind = "wait"
	KindBash Kind = "bash" // anything not recognized
)

// priority orders kinds for chained commands: the highest wins.
var priority = map[Kind]int{
	KindBash:    0,
	KindVCS:     1,
	KindWait:    2,
	KindPackage: 3,
	KindLint:    4,
	KindBuild:   5,
	KindTest:    6,
}

// Valid reports whether k is one of the defined kinds.
func (k Kind) Valid() bool {
	_, ok := priority[k]
	return ok
}
