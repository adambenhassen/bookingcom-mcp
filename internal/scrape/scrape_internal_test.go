package scrape

import "testing"

func TestCap(t *testing.T) {
	s := []int{1, 2, 3, 4, 5}
	cases := []struct {
		n    int
		want int
	}{
		{3, 3},  // truncate
		{5, 5},  // n == len: passthrough (no off-by-one)
		{10, 5}, // n > len: passthrough
		{0, 5},  // n <= 0: no limit
		{-1, 5}, // negative: no limit
	}
	for _, c := range cases {
		if got := Cap(s, c.n); len(got) != c.want {
			t.Errorf("Cap(len=5, n=%d) len=%d, want %d", c.n, len(got), c.want)
		}
	}
}
