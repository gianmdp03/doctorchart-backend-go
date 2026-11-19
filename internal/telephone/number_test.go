package telephone

import "testing"

func TestNormalize(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		valid       bool
	}{
		{"", "", true},
		{" \t ", "", true},
		{"011 15-2345-6789", "+5491123456789", true},
		{"0351 15-234-5678", "+5493512345678", true},
		{"0223 495-1234", "+542234951234", true},
		{"+54 9 11 2345-6789", "+5491123456789", true},
		{"00 34 612 345 678", "+34612345678", true},
		{"+1 (416) 555-0123", "+14165550123", true},
		{"+1 (202) 555-0123", "+12025550123", true},
		{"+6834000", "+6834000", true},
		{"223 1234567", "", false},
		{"123", "", false},
		{"+999123456789", "", false},
		{"+541122223333444455", "", false},
		{"+54 11 2345-6789 ext. 100", "", false},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, valid := Normalize(tc.input)
			if got != tc.want || valid != tc.valid {
				t.Fatalf("Normalize(%q) = (%q, %v), want (%q, %v)", tc.input, got, valid, tc.want, tc.valid)
			}
		})
	}
}
