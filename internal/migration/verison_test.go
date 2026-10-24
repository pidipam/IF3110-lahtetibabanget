package migration

import "testing"

func TestNextVersion(t *testing.T) {
	cases := []struct{ in, want int }{{0, 1}, {1, 2}, {41, 42}, {-5, 1}}
	for _, c := range cases {
		if got := NextVersion(c.in); got != c.want {
			t.Errorf("NextVersion(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}