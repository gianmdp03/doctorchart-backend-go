package clinics

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestStaffPasswordLength(t *testing.T) {
	for _, tc := range []struct {
		name  string
		raw   string
		valid bool
	}{
		{"empty", "", false},
		{"five characters", "abcde", false},
		{"six characters", "abcdef", true},
		{"five multibyte characters", "áéíóú", false},
		{"six multibyte characters", "áéíóúñ", true},
		{"bcrypt byte limit", strings.Repeat("a", 72), true},
		{"over byte limit", strings.Repeat("a", 73), false},
		{"multibyte over byte limit", strings.Repeat("á", 37), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := staffPassword(tc.raw)
			if !tc.valid {
				if err == nil || hash != nil {
					t.Fatal("invalid password was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := bcrypt.CompareHashAndPassword(hash, []byte(tc.raw)); err != nil {
				t.Fatal("password hash does not verify:", err)
			}
			if cost, err := bcrypt.Cost(hash); err != nil || cost != 12 {
				t.Fatalf("unexpected bcrypt cost: %d, %v", cost, err)
			}
		})
	}
}
