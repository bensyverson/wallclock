package classify

import "strings"

// splitSegments splits a command line into simple commands, each a list of
// words with quotes removed. Segments break at && || ; | & newlines and
// parentheses. Redirections, comments and heredoc bodies are dropped, so
// nothing inside them can be mistaken for a command.
func splitSegments(s string) [][]string {
	l := &lexer{s: s}
	l.run()
	return l.segs
}

type heredoc struct {
	delim string
	dash  bool // <<- strips leading tabs from the terminator line
}

type lexer struct {
	s        string
	i        int
	segs     [][]string
	cur      []string
	word     strings.Builder
	inWord   bool
	quoted   bool // current word contains quoted or escaped text
	skipNext bool // previous word was a bare redirection operator
	pending  []heredoc
}

func (l *lexer) run() {
	for l.i < len(l.s) {
		c := l.s[l.i]
		switch {
		case c == '\\':
			l.escape()
		case c == '\'':
			l.singleQuote()
		case c == '"':
			l.doubleQuote()
		case c == '`':
			l.balanced('`', '`')
		case c == '$' && l.peek(1) == '(':
			l.add('$')
			l.i++
			l.balanced('(', ')')
		case c == ' ' || c == '\t' || c == '\r':
			l.flushWord()
			l.i++
		case c == '\n':
			l.endSegment()
			l.i++
			l.skipHeredocBodies()
		case c == ';' || c == '(' || c == ')':
			l.endSegment()
			l.i++
		case c == '|':
			l.endSegment()
			l.i++
			if l.peek(0) == '|' || l.peek(0) == '&' {
				l.i++
			}
		case c == '&':
			if l.peek(1) == '>' || (l.inWord && !l.quoted && l.endsWithRedirect()) {
				l.add(c)
				l.i++
				continue
			}
			l.endSegment()
			l.i++
			if l.peek(0) == '&' {
				l.i++
			}
		case c == '#' && !l.inWord:
			for l.i < len(l.s) && l.s[l.i] != '\n' {
				l.i++
			}
		case c == '<' && l.peek(1) == '<' && l.peek(2) != '<':
			l.flushWord()
			l.i += 2
			l.heredocStart()
		default:
			l.add(c)
			l.i++
		}
	}
	l.endSegment()
}

func (l *lexer) peek(n int) byte {
	if l.i+n < len(l.s) {
		return l.s[l.i+n]
	}
	return 0
}

func (l *lexer) add(c byte) {
	l.word.WriteByte(c)
	l.inWord = true
}

func (l *lexer) endsWithRedirect() bool {
	w := l.word.String()
	return strings.HasSuffix(w, ">") || strings.HasSuffix(w, "<")
}

func (l *lexer) escape() {
	if l.peek(1) == '\n' {
		l.i += 2
		return
	}
	if l.i+1 < len(l.s) {
		l.add(l.s[l.i+1])
		l.quoted = true
	}
	l.i += 2
}

func (l *lexer) singleQuote() {
	l.inWord, l.quoted = true, true
	l.i++
	for l.i < len(l.s) && l.s[l.i] != '\'' {
		l.word.WriteByte(l.s[l.i])
		l.i++
	}
	l.i++
}

func (l *lexer) doubleQuote() {
	l.inWord, l.quoted = true, true
	l.i++
	for l.i < len(l.s) && l.s[l.i] != '"' {
		c := l.s[l.i]
		if c == '\\' && l.i+1 < len(l.s) && strings.IndexByte("\"\\$`", l.s[l.i+1]) >= 0 {
			l.i++
			c = l.s[l.i]
		}
		l.word.WriteByte(c)
		l.i++
	}
	l.i++
}

// balanced copies a command substitution (or backtick span) into the current
// word verbatim; l.i sits on the opening delimiter.
func (l *lexer) balanced(open, closer byte) {
	l.inWord, l.quoted = true, true
	depth := 0
	for l.i < len(l.s) {
		c := l.s[l.i]
		l.word.WriteByte(c)
		l.i++
		if open == closer {
			if c == open {
				depth++
				if depth == 2 {
					return
				}
			}
			continue
		}
		switch c {
		case open:
			depth++
		case closer:
			depth--
			if depth == 0 {
				return
			}
		}
	}
}

func (l *lexer) flushWord() {
	if !l.inWord {
		return
	}
	text, quoted := l.word.String(), l.quoted
	l.word.Reset()
	l.inWord, l.quoted = false, false
	if l.skipNext {
		l.skipNext = false
		return
	}
	if !quoted {
		if rest, ok := redirectRest(text); ok {
			l.skipNext = rest == ""
			return
		}
	}
	l.cur = append(l.cur, text)
}

// redirectRest reports whether w is a redirection such as 2>&1, >file or <,
// and returns whatever operand is attached to the operator.
func redirectRest(w string) (string, bool) {
	i := 0
	for i < len(w) && (w[i] >= '0' && w[i] <= '9' || w[i] == '&') {
		i++
	}
	if i >= len(w) || (w[i] != '>' && w[i] != '<') {
		return "", false
	}
	for i < len(w) && (w[i] == '>' || w[i] == '<') {
		i++
	}
	if i < len(w) && w[i] == '&' {
		i++
		if i == len(w) {
			return "-", true
		}
	}
	return w[i:], true
}

func (l *lexer) endSegment() {
	l.flushWord()
	l.skipNext = false
	if len(l.cur) > 0 {
		l.segs = append(l.segs, l.cur)
		l.cur = nil
	}
}

// heredocStart parses the delimiter after <<; l.i sits just past the operator.
func (l *lexer) heredocStart() {
	h := heredoc{}
	if l.peek(0) == '-' {
		h.dash = true
		l.i++
	}
	for l.peek(0) == ' ' || l.peek(0) == '\t' {
		l.i++
	}
	var d strings.Builder
	for l.i < len(l.s) {
		c := l.s[l.i]
		if c == '\'' || c == '"' {
			end := strings.IndexByte(l.s[l.i+1:], c)
			if end < 0 {
				l.i = len(l.s)
				break
			}
			d.WriteString(l.s[l.i+1 : l.i+1+end])
			l.i += end + 2
			continue
		}
		if c == '\\' && l.i+1 < len(l.s) {
			d.WriteByte(l.s[l.i+1])
			l.i += 2
			continue
		}
		if strings.IndexByte(" \t\r\n;&|()<>", c) >= 0 {
			break
		}
		d.WriteByte(c)
		l.i++
	}
	h.delim = d.String()
	l.pending = append(l.pending, h)
}

// skipHeredocBodies consumes lines through each pending terminator; l.i sits
// at the start of the first body line.
func (l *lexer) skipHeredocBodies() {
	for _, h := range l.pending {
		for l.i < len(l.s) {
			end := strings.IndexByte(l.s[l.i:], '\n')
			line := l.s[l.i:]
			if end >= 0 {
				line = l.s[l.i : l.i+end]
				l.i += end + 1
			} else {
				l.i = len(l.s)
			}
			line = strings.TrimSuffix(line, "\r")
			if h.dash {
				line = strings.TrimLeft(line, "\t")
			}
			if line == h.delim {
				break
			}
		}
	}
	l.pending = nil
}
