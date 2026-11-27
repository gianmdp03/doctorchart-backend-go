package clinical

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

func testSignatureAsset(id int64, name, stamp, sha string) *workspace.SignatureAsset {
	img := image.NewNRGBA(image.Rect(0, 0, 100, 40))
	for x := 10; x < 90; x++ {
		img.SetNRGBA(x, 20, color.NRGBA{R: 15, G: 23, B: 42, A: 255})
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return &workspace.SignatureAsset{
		ID:        id,
		Name:      name,
		StampText: stamp,
		ImagePNG:  buf.Bytes(),
		Width:     100,
		Height:    40,
		SHA256:    sha,
		Favorite:  true,
	}
}

func TestPDFCacheKeyAndInvalidationOnSignatureChange(t *testing.T) {
	zone := time.UTC
	service := &Service{
		zone:       zone,
		pdfCache:   newPDFCache(8, 8<<20, time.Minute),
		pdfLimiter: newPDFLimiter(time.Minute, 100),
	}
	doc := printDocument{
		Source:       "PRESCRIPTION_RECORD",
		ID:           10,
		Code:         "RX-10-1",
		at:           time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC),
		IssuanceZone: "UTC",
		Patient:      printPatient{Name: "Sofía Martín", Insurance: json.RawMessage("null")},
		Author:       printAuthor{UserID: "doc-cache-1", name: "Dra. Paula Gómez"},
		Medications:  json.RawMessage(`[{"medicationName":"Amoxicilina 500 mg"}]`),
		signature:    testSignatureAsset(1, "Paula", "Dra. Paula Gómez — MN 11111", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}

	first, err := service.printPDF(doc, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.printPDF(doc, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("expected identical PDF bytes from cache hit")
	}

	doc.signature = testSignatureAsset(2, "Julieta", "Dra. Julieta Pérez — MN 22222", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	third, err := service.printPDF(doc, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, third) {
		t.Fatal("expected cache miss and new PDF when signature changed")
	}
	if !projectionFixturePDFContains(t, third, "Dra. Julieta Pérez") {
		t.Fatal("new PDF did not contain updated signature stamp")
	}
}

func TestPDFLimiterSkipsCacheHitsAndLimitsMisses(t *testing.T) {
	limiter := newPDFLimiter(time.Minute, 2)
	now := time.Now()
	if !limiter.allow("doc-1", now) || !limiter.allow("doc-1", now.Add(time.Second)) {
		t.Fatal("expected initial misses to be allowed")
	}
	if limiter.allow("doc-1", now.Add(2*time.Second)) {
		t.Fatal("expected third miss within window to be rate-limited")
	}
	if !limiter.allow("doc-1", now.Add(time.Minute+time.Second)) {
		t.Fatal("expected limiter to allow after window expires")
	}

	service := &Service{
		zone:       time.UTC,
		pdfCache:   newPDFCache(8, 8<<20, time.Minute),
		pdfLimiter: newPDFLimiter(time.Minute, 1),
	}
	doc := printDocument{
		Source:       "PRESCRIPTION_RECORD",
		ID:           1,
		Code:         "RX-1-1",
		at:           now,
		IssuanceZone: "UTC",
		Patient:      printPatient{Name: "Sofía Martín", Insurance: json.RawMessage("null")},
		Author:       printAuthor{UserID: "doc-rate-1", name: "Dra. Paula Gómez"},
		Medications:  json.RawMessage(`[]`),
		Orders:       []string{"Laboratorio"},
	}
	if _, err := service.printPDF(doc, url.Values{}); err != nil {
		t.Fatal(err)
	}
	// Cache hit must not consume rate-limiter quota.
	if _, err := service.printPDF(doc, url.Values{}); err != nil {
		t.Fatalf("cache hit was unexpectedly rate limited: %v", err)
	}
	// Cache miss after quota exhausted must return 429.
	doc.ID = 2
	_, err := service.printPDF(doc, url.Values{})
	failure, ok := err.(*api.Error)
	if !ok || failure.Status != 429 || failure.Code != "PDF_RATE_LIMITED" {
		t.Fatalf("expected 429 PDF_RATE_LIMITED, got %v", err)
	}
}
