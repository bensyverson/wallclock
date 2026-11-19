package classify

import (
	"reflect"
	"testing"
)

func TestSplitSegments(t *testing.T) {
	cases := []struct {
		in   string
		want [][]string
	}{
		{"go test ./...", [][]string{{"go", "test", "./..."}}},
		{"a && b || c; d | e & f", [][]string{{"a"}, {"b"}, {"c"}, {"d"}, {"e"}, {"f"}}},
		{`echo "a b" 'c d'`, [][]string{{"echo", "a b", "c d"}}},
		{`echo a" b"c`, [][]string{{"echo", "a bc"}}},
		{`echo "a && b"`, [][]string{{"echo", "a && b"}}},
		{`echo \&\& x`, [][]string{{"echo", "&&", "x"}}},
		{"go test 2>&1 | tail", [][]string{{"go", "test"}, {"tail"}}},
		{"go test > out.txt", [][]string{{"go", "test"}}},
		{"go test &> out.txt", [][]string{{"go", "test"}}},
		{"cat <<EOF\nbody; go test\nEOF\nls", [][]string{{"cat"}, {"ls"}}},
		{"cat <<-EOF\n\tbody\n\tEOF\nls", [][]string{{"cat"}, {"ls"}}},
		{"ls # comment && go test", [][]string{{"ls"}}},
		{"a\\\nb", [][]string{{"ab"}}},
		{"x=$(a && b) go", [][]string{{"x=$(a && b)", "go"}}},
		{"(a && b)", [][]string{{"a"}, {"b"}}},
		{"", nil},
	}
	for _, tc := range cases {
		got := splitSegments(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitSegments(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
