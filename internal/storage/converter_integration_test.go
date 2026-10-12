package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/image/webp"
)

func liveConverter(t *testing.T) ConverterConfig {
	t.Helper()
	address, key := os.Getenv("DOCTORCHART_CONVERTER_TEST_URL"), os.Getenv("DOCTORCHART_CONVERTER_TEST_API_KEY")
	if address == "" || key == "" {
		t.Skip("local converter fixture not configured")
	}
	u, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		t.Fatal("integration test requires a loopback converter")
	}
	return ConverterConfig{URL: address, APIKey: key, AllowStudyTransformation: true}
}

func noisyStudy(w, h int, alpha bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	seed := uint32(7)
	for i := 0; i < len(img.Pix); i += 4 {
		seed = seed*1664525 + 1013904223
		a := uint8(255)
		if alpha {
			a = uint8(seed >> 24)
		}
		copy(img.Pix[i:i+4], []byte{uint8(seed), uint8(seed >> 8), uint8(seed >> 16), a})
	}
	return img
}

func TestLiveConverterLocalStorage(t *testing.T) {
	c := liveConverter(t)
	for _, alpha := range []bool{false, true} {
		t.Run(map[bool]string{false: "opaque", true: "transparent"}[alpha], func(t *testing.T) {
			var input bytes.Buffer
			if err := png.Encode(&input, noisyStudy(2400, 1000, alpha)); err != nil {
				t.Fatal(err)
			}
			store, err := New(Config{Directory: privateStudyDirectory(t), Converter: c})
			if err != nil {
				t.Fatal(err)
			}
			saved, err := store.Save(context.Background(), "study.png", bytes.NewReader(input.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(store.root, saved.Key))
			if err != nil {
				t.Fatal(err)
			}
			img, err := webp.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if saved.Name != "study.webp" || saved.ContentType != "image/webp" || saved.Size != int64(len(data)) || len(data) >= input.Len() || img.Bounds().Dx() != 2048 || img.Bounds().Dy() != 853 {
				t.Fatalf("incorrect conversion metadata: %+v %v", saved, img.Bounds())
			}
			if alpha {
				_, _, _, a := img.At(100, 100).RGBA()
				if a == 65535 {
					t.Fatal("alpha lost")
				}
			}
			t.Logf("input=%d stored=%d dimensions=%v", input.Len(), len(data), img.Bounds())
		})
	}
}

func TestLiveConverterOrientationAndSpecialPreservation(t *testing.T) {
	store, err := New(Config{Directory: privateStudyDirectory(t), Converter: liveConverter(t)})
	if err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	if err := jpeg.Encode(&input, image.NewRGBA(image.Rect(0, 0, 3000, 1000)), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	exif := []byte("Exif\x00\x00II*\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00\x06\x00\x00\x00\x00\x00\x00\x00")
	segment := []byte{255, 225, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(exif)+2))
	original := append([]byte{}, input.Bytes()[:2]...)
	original = append(original, segment...)
	original = append(original, exif...)
	original = append(original, input.Bytes()[2:]...)
	saved, err := store.Save(context.Background(), "orientation.jpg", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(store.root, saved.Key))
	if err != nil {
		t.Fatal(err)
	}
	config, err := webp.DecodeConfig(bytes.NewReader(actual))
	if err != nil || config.Width != 682 || config.Height != 2048 {
		t.Fatalf("orientation ignored: %+v %v", config, err)
	}
	high := image.NewGray16(image.Rect(0, 0, 8, 8))
	high.SetGray16(1, 1, color.Gray16{Y: 12345})
	var special bytes.Buffer
	if err := png.Encode(&special, high); err != nil {
		t.Fatal(err)
	}
	saved, err = store.Save(context.Background(), "high-depth.png", bytes.NewReader(special.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	actual, err = os.ReadFile(filepath.Join(store.root, saved.Key))
	if err != nil || !bytes.Equal(actual, special.Bytes()) {
		t.Fatal("high precision file modified")
	}
}

func TestLiveConverterS3AndApplicationIsolation(t *testing.T) {
	c := liveConverter(t)
	var uploads atomic.Int32
	var uploaded []byte
	var contentType string
	var length int64
	s3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Error("unexpected S3 method")
		}
		uploads.Add(1)
		var err error
		uploaded, err = io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		contentType = r.Header.Get("Content-Type")
		length = r.ContentLength
		w.Header().Set("ETag", `"fixture"`)
	}))
	defer s3.Close()
	store, err := New(Config{Type: "s3", Endpoint: s3.URL, Bucket: "owned-fixture", AccessKey: "fixture", SecretKey: "fixture", Converter: c})
	if err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	if err := png.Encode(&input, noisyStudy(800, 800, false)); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(context.Background(), "study.png", bytes.NewReader(input.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if uploads.Load() != 1 || len(uploaded) >= input.Len() || saved.ContentType != "image/webp" || contentType != saved.ContentType || length != saved.Size || len(uploaded) != int(saved.Size) {
		t.Fatal("remote conversion/S3 protocol mismatch")
	}
	pointlyKey := os.Getenv("DOCTORCHART_CONVERTER_TEST_POINTLY_KEY")
	if pointlyKey == "" {
		t.Fatal("separate Pointly fixture credential required")
	}
	c.APIKey = pointlyKey
	denied, err := New(Config{Type: "s3", Endpoint: s3.URL, Bucket: "owned-fixture", AccessKey: "fixture", SecretKey: "fixture", Converter: c})
	if err != nil {
		t.Fatal(err)
	}
	_, err = denied.Save(context.Background(), "study.png", bytes.NewReader(input.Bytes()))
	if err == nil || uploads.Load() != 1 || strings.Contains(err.Error(), pointlyKey) {
		t.Fatal("cross-application credential wrote an object")
	}
}
