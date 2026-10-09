package clinical

import "testing"

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
