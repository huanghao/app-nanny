package daemon

import "testing"

func TestParseClock(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0:00.00", 0},
		{"00:00", 0},
		{"53.17", 53.17},
		{"310:53.17", 310*60 + 53.17},
		{"03:22:32", (3*60+22)*60 + 32},
		{"36-03:22:32", 36*86400 + (3*60+22)*60 + 32},
	}
	for _, c := range cases {
		got := parseClock(c.in)
		if diff := got - c.want; diff > 0.001 || diff < -0.001 {
			t.Errorf("parseClock(%q) = %f, want %f", c.in, got, c.want)
		}
	}
}
