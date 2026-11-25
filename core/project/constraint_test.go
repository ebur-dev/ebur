package project

import "testing"

func TestConstraints(t *testing.T) {
	cases := []struct {
		c   string
		yes []string
		no  []string
	}{
		{"^8.2", []string{"8.2.0", "8.4.28", "8.6.0"}, []string{"8.1.34", "9.0.0", "7.4.33"}},
		{"^8.2.5", []string{"8.2.5", "8.5.13"}, []string{"8.2.4", "9.0.0"}},
		{"^0.3", []string{"0.3.9"}, []string{"0.4.0"}},
		{"~8.2", []string{"8.2.0", "8.6.0"}, []string{"8.1.0", "9.0.0"}},
		{"~8.2.1", []string{"8.2.1", "8.2.36"}, []string{"8.2.0", "8.3.0"}},
		{">=8.1 <8.4", []string{"8.1.0", "8.3.37"}, []string{"8.0.30", "8.4.0"}},
		{">=8.1,<8.4", []string{"8.2.0"}, []string{"8.4.28"}},
		{">= 8.1 < 8.4", []string{"8.2.0"}, []string{"8.4.28"}},
		{"^7.4|^8.0", []string{"7.4.33", "8.5.13"}, []string{"7.3.0", "9.0.0"}},
		{"^7.4 || ^8.0", []string{"8.0.0"}, []string{"9.1.0"}},
		{"8.2.*", []string{"8.2.0", "8.2.36"}, []string{"8.3.0"}},
		{"8.*", []string{"8.0.0", "8.6.0"}, []string{"9.0.0"}},
		{"8.3", []string{"8.3.0", "8.3.37"}, []string{"8.4.0"}},
		{"8.3.37", []string{"8.3.37"}, []string{"8.3.36"}},
		{"=8.3.37", []string{"8.3.37"}, []string{"8.3.38"}},
		{"8.1 - 8.3", []string{"8.1.0", "8.3.99"}, []string{"8.4.0", "8.0.9"}},
		{"8.1.2 - 8.3.4", []string{"8.3.4"}, []string{"8.3.5"}},
		{"*", []string{"5.6.0", "8.6.0"}, nil},
		{">8.2", []string{"8.2.1"}, []string{"8.2.0"}},
		{"<=8.2", []string{"8.2.0"}, []string{"8.2.1"}},
		{"!=8.3.0", []string{"8.3.1"}, []string{"8.3.0"}},
		{"^8.2@dev", []string{"8.4.0"}, []string{"8.1.0"}},
		{"v8.2.*", []string{"8.2.1"}, []string{"8.3.0"}},
	}
	for _, tc := range cases {
		c, err := ParseConstraint(tc.c)
		if err != nil {
			t.Errorf("%q: %v", tc.c, err)
			continue
		}
		for _, v := range tc.yes {
			if !Satisfies(c, v) {
				t.Errorf("%q should allow %s", tc.c, v)
			}
		}
		for _, v := range tc.no {
			if Satisfies(c, v) {
				t.Errorf("%q should not allow %s", tc.c, v)
			}
		}
	}
}

func TestBadConstraints(t *testing.T) {
	for _, bad := range []string{"", "^", "abc", "8.x.y.z.w", ">=8.1 |", "| ^8"} {
		if _, err := ParseConstraint(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
