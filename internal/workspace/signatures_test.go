package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func makeTestSignaturePNG(w, h int, inkCount int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	drawn := 0
	for y := 0; y < h && drawn < inkCount; y++ {
		for x := 0; x < w && drawn < inkCount; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 15, G: 23, B: 42, A: 255})
			drawn++
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestSanitizeSignaturePNGValidAndReencoded(t *testing.T) {
	raw := makeTestSignaturePNG(120, 48, 80)
	clean, w, h, digest, err := sanitizeSignaturePNG(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 120 || h != 48 {
		t.Fatalf("unexpected dimensions %dx%d", w, h)
	}
	if fmt.Sprintf("%x", sha256.Sum256(clean)) != digest {
		t.Fatal("sha256 mismatch")
	}
	if _, err := png.Decode(bytes.NewReader(clean)); err != nil {
		t.Fatalf("reencoded PNG invalid: %v", err)
	}
}

func TestSanitizeSignaturePNGRejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name   string
		raw    []byte
		status int
		code   string
	}{
		{"empty bytes", []byte{}, 400, "INVALID_SIGNATURE_IMAGE"},
		{"non-png bytes", []byte("not a png"), 400, "INVALID_SIGNATURE_IMAGE"},
		{"oversized bytes", make([]byte, maxSignaturePNGBytes+1), 413, "SIGNATURE_TOO_LARGE"},
		{"too wide", makeTestSignaturePNG(maxSignatureWidth+1, 20, 40), 400, "INVALID_SIGNATURE_DIMENSIONS"},
		{"too tall", makeTestSignaturePNG(20, maxSignatureHeight+1, 40), 400, "INVALID_SIGNATURE_DIMENSIONS"},
		{"transparent / no ink", makeTestSignaturePNG(80, 40, 2), 400, "EMPTY_SIGNATURE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, err := sanitizeSignaturePNG(tc.raw)
			failure, ok := err.(*api.Error)
			if !ok || failure.Status != tc.status || failure.Code != tc.code {
				t.Fatalf("got %v, want status=%d code=%s", err, tc.status, tc.code)
			}
		})
	}
}

func TestDecodeSignatureImageField(t *testing.T) {
	raw := makeTestSignaturePNG(40, 20, 30)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	got, err := decodeSignatureImageField(dataURL)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("failed to decode data URL: %v", err)
	}
	if _, err := decodeSignatureImageField("data:image/jpeg;base64,/9j/4AAQ"); err == nil {
		t.Fatal("expected rejection for non-png data URL")
	}
	if _, err := decodeSignatureImageField("   "); err == nil {
		t.Fatal("expected rejection for blank field")
	}
}
