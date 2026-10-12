package config

import (
	"strings"
	"testing"
)

func TestSharedSecretPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"missing", "", false}, {"31 characters", strings.Repeat("a", 31), false},
		{"32 characters", strings.Repeat("a", 32), true},
		{"31 Unicode characters", strings.Repeat("😀", 31), false},
		{"32 Unicode characters", strings.Repeat("😀", 32), true},
		{"leading whitespace", " " + strings.Repeat("a", 32), false},
		{"trailing whitespace", strings.Repeat("a", 32) + "\t", false},
		{"example", strings.Repeat("a", 32) + "EXAMPLE", false},
		{"replacement", strings.Repeat("a", 32) + "REPLACE", false},
		{"old default", "shared-internal-secret", false},
		{"invalid Unicode", strings.Repeat("a", 32) + string([]byte{0xff}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSharedSecret("GATEWAY_SHARED_SECRET", tc.value)
			if (err == nil) != tc.valid {
				t.Fatal("shared secret policy differs")
			}
			if err != nil && len(tc.value) > 8 && strings.Contains(err.Error(), tc.value) {
				t.Fatal("credential exposed")
			}
		})
	}
}
