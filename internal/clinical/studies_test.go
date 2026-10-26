package clinical

import (
	"errors"
	"strings"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestStudyNameValidation(t *testing.T) {
	for _, test := range []struct {
		label  string
		values []string
		want   string
		code   string
	}{
		{"missing", nil, "", "STUDY_NAME_REQUIRED"},
		{"blank", []string{" \u00a0 "}, "", "STUDY_NAME_REQUIRED"},
		{"duplicate", []string{"one", "two"}, "", "INVALID_STUDY_NAME"},
		{"long", []string{strings.Repeat("a", 241)}, "", "STUDY_NAME_TOO_LONG"},
		{"utf16 long", []string{strings.Repeat("😀", 121)}, "", "STUDY_NAME_TOO_LONG"},
		{"control", []string{"Control\n"}, "", "INVALID_STUDY_NAME"},
		{"bidi", []string{"Control\u202e"}, "", "INVALID_STUDY_NAME"},
		{"invalid utf8", []string{string([]byte{0xff})}, "", "INVALID_STUDY_NAME"},
		{"clinical title", []string{`  Tomografía "control" / cerebral: anual  `}, `Tomografía "control" / cerebral: anual`, ""},
		{"literal extension", []string{"Informe.pdf"}, "Informe.pdf", ""},
		{"boundary", []string{strings.Repeat("a", 240)}, strings.Repeat("a", 240), ""},
		{"utf16 boundary", []string{strings.Repeat("😀", 120)}, strings.Repeat("😀", 120), ""},
	} {
		t.Run(test.label, func(t *testing.T) {
			got, err := studyName(test.values)
			if test.code == "" {
				if err != nil || got != test.want {
					t.Fatalf("got %q, %v; want %q", got, err, test.want)
				}
				return
			}
			var failure *api.Error
			if !errors.As(err, &failure) || failure.Status != 400 || failure.Code != test.code {
				t.Fatalf("got %v; want %s", err, test.code)
			}
		})
	}
}

func TestStudyDisposition(t *testing.T) {
	for _, test := range []struct{ name, expected string }{
		{"estudio clínico.png", "inline; filename=\"estudio clinico.png\"; filename*=UTF-8''estudio%20cl%C3%ADnico.png"},
		{"ÄÖÜßæŒ_日本.png", "inline; filename=\"AeOeUessaeOE___.png\"; filename*=UTF-8''%C3%84%C3%96%C3%9C%C3%9F%C3%A6%C5%92_%E6%97%A5%E6%9C%AC.png"},
		{"a+b&c.png", "inline; filename=\"a+b&c.png\"; filename*=UTF-8''a+b&c.png"},
	} {
		if actual := studyDisposition(test.name); actual != test.expected {
			t.Errorf("%q: %q, want %q", test.name, actual, test.expected)
		}
	}
}
