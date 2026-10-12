package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

const converterTestKey = "doctorchart-converter-test-key-000001"

func TestTenMBStudyUploadBoundary(t *testing.T) {
	base := studyPNG(t, 2, 2)
	chunk := make([]byte, MaxBytes-len(base))
	binary.BigEndian.PutUint32(chunk, uint32(len(chunk)-12))
	copy(chunk[4:8], "ruSt")
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	data := append(append(append([]byte{}, base[:len(base)-12]...), chunk...), base[len(base)-12:]...)
	var calls atomic.Int32
	converter := fixtureConverter(t, func(w http.ResponseWriter, r *http.Request, original []byte) {
		calls.Add(1)
		writeConverted(w, original, "image/png", true)
	})
	directory := privateStudyDirectory(t)
	store, err := New(Config{Directory: directory, Converter: converter})
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.Save(context.Background(), "study.png", bytes.NewReader(data))
	if err != nil || file.Size != 10_000_000 || calls.Load() != 1 {
		t.Fatalf("10 MB original rejected: %+v %v", file, err)
	}
	_, err = store.Save(context.Background(), "study.png", bytes.NewReader(append(data, 0)))
	var failure *api.Error
	if !errors.As(err, &failure) || failure.Status != 413 || failure.Code != "STUDY_FILE_TOO_LARGE" || calls.Load() != 1 {
		t.Fatalf("oversized original reached converter: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatal("oversized upload created an object")
	}
}

func studyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func fixtureConverter(t *testing.T, handler func(http.ResponseWriter, *http.Request, []byte)) ConverterConfig {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/convert" || r.Header.Get("Authorization") != "Bearer "+converterTestKey {
			t.Error("invalid backend converter authentication/route")
			w.WriteHeader(401)
			return
		}
		if r.Header.Get("X-Gateway-Secret") != "" || r.Header.Get("X-User-Id") != "" {
			t.Error("forwarded user/Gateway identity")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("profile") != studyProfile || len(r.MultipartForm.Value) != 1 {
			t.Error("profile or image options are caller-controlled")
			w.WriteHeader(400)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer file.Close()
		if header.Filename != "original" {
			t.Error("medical filename leaked to converter")
		}
		data, err := io.ReadAll(file)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		handler(w, r, data)
	}))
	t.Cleanup(server.Close)
	return ConverterConfig{URL: server.URL, APIKey: converterTestKey, AllowStudyTransformation: true}
}

func writeConverted(w http.ResponseWriter, data []byte, mimeType string, preserved bool) {
	c, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-Image-Profile", studyProfile)
	w.Header().Set("X-Image-Width", strconv.Itoa(c.Width))
	w.Header().Set("X-Image-Height", strconv.Itoa(c.Height))
	w.Header().Set("X-Image-Preserved", strconv.FormatBool(preserved))
	w.Write(data)
}

func preservingConverter(t *testing.T) ConverterConfig {
	t.Helper()
	c := fixtureConverter(t, func(w http.ResponseWriter, r *http.Request, data []byte) {
		_, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			w.WriteHeader(400)
			return
		}
		writeConverted(w, data, "image/"+format, true)
	})
	c.AllowStudyTransformation = false
	return c
}

func TestStudyConversionDelegatesAndStoresResponse(t *testing.T) {
	input := studyPNG(t, 80, 40)
	output := studyPNG(t, 8, 4)
	var calls atomic.Int32
	c := fixtureConverter(t, func(w http.ResponseWriter, r *http.Request, data []byte) {
		calls.Add(1)
		if !bytes.Equal(data, input) {
			t.Error("original bytes changed before API call")
		}
		writeConverted(w, output, "image/png", false)
	})
	store, err := New(Config{Directory: privateStudyDirectory(t), Converter: c})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(context.Background(), "medical.png", bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(store.root, saved.Key))
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || !bytes.Equal(actual, output) || saved.Size != int64(len(output)) || saved.ContentType != "image/png" {
		t.Fatal("converter response was not stored exactly")
	}
}

func TestS3StoresOnlyConvertedStudyAndMetadata(t *testing.T) {
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	picture := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	seed := uint32(7)
	for i := range picture.Pix {
		seed = seed*1664525 + 1013904223
		picture.Pix[i] = uint8(seed >> 24)
	}
	var original bytes.Buffer
	if err := png.Encode(&original, picture); err != nil {
		t.Fatal(err)
	}
	input := original.Bytes()
	if len(input) <= jpg.Len() {
		t.Fatal("fixture must allow compression")
	}
	var converted atomic.Bool
	c := fixtureConverter(t, func(w http.ResponseWriter, r *http.Request, data []byte) {
		converted.Store(true)
		writeConverted(w, jpg.Bytes(), "image/jpeg", false)
	})
	var uploaded []byte
	var uploadedType string
	var uploadedSize int64
	var requests atomic.Int32
	s3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "PUT" || !converted.Load() {
			t.Error("storage called before conversion")
		}
		var err error
		uploaded, err = io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		uploadedType = r.Header.Get("Content-Type")
		uploadedSize = r.ContentLength
		w.Header().Set("ETag", `"fixture"`)
	}))
	defer s3.Close()
	store, err := New(Config{Type: "s3", Endpoint: s3.URL, Bucket: "fixture", AccessKey: "fixture", SecretKey: "fixture", Converter: c})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(context.Background(), "study.png", bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || !bytes.Equal(uploaded, jpg.Bytes()) || uploadedType != "image/jpeg" || saved.ContentType != uploadedType || saved.Size != uploadedSize || saved.Size != int64(len(uploaded)) || saved.Name != "study.jpg" {
		t.Fatal("S3 metadata/body mismatch")
	}
}

func TestConverterFailuresNeverPersistOrExposeSecrets(t *testing.T) {
	data := studyPNG(t, 8, 8)
	for _, status := range []int{400, 401, 403, 413, 415, 500, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			c := fixtureConverter(t, func(w http.ResponseWriter, r *http.Request, data []byte) {
				w.WriteHeader(status)
				fmt.Fprintf(w, `{"error":{"code":"remote_error","message":%q}}`, converterTestKey)
			})
			directory := privateStudyDirectory(t)
			store, err := New(Config{Directory: directory, Converter: c})
			if err != nil {
				t.Fatal(err)
			}
			file, err := store.Save(context.Background(), "study.png", bytes.NewReader(data))
			if err == nil || file.Key != "" {
				t.Fatal("failed conversion persisted")
			}
			var failure *api.Error
			if !errors.As(err, &failure) || strings.Contains(err.Error(), converterTestKey) || strings.Contains(err.Error(), c.URL) {
				t.Fatalf("unsafe error: %v", err)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 {
				t.Fatal("conversion failure wrote a file")
			}
		})
	}
}

func TestSpecialStudyPreservationAndUnsupportedBypass(t *testing.T) {
	data := studyPNG(t, 8, 8)
	for _, code := range []string{"special_image", "animated_image"} {
		c := fixtureConverter(t, func(w http.ResponseWriter, r *http.Request, data []byte) {
			w.WriteHeader(415)
			fmt.Fprintf(w, `{"error":{"code":%q}}`, code)
		})
		store, err := New(Config{Directory: privateStudyDirectory(t), Converter: c})
		if err != nil {
			t.Fatal(err)
		}
		actual, name, mimeType, err := store.convertStudy(context.Background(), data, "study.png", "image/png")
		if err != nil || !bytes.Equal(actual, data) || name != "study.png" || mimeType != "image/png" {
			t.Fatal("special study was not preserved")
		}
	}
	store := &Store{}
	for _, mimeType := range []string{"application/pdf", "image/gif", "image/tiff", "image/bmp"} {
		actual, _, _, err := store.convertStudy(context.Background(), data, "original", mimeType)
		if err != nil || !bytes.Equal(actual, data) {
			t.Fatal("unsupported attachment did not retain original path")
		}
	}
	_, _, _, err := store.convertStudy(context.Background(), data, "study.png", "image/png")
	var failure *api.Error
	if !errors.As(err, &failure) || failure.Code != "IMAGE_CONVERTER_UNAVAILABLE" {
		t.Fatal("missing converter silently accepted image")
	}
}

func TestConverterRejectsInvalidResponsesAndUnapprovedTransformation(t *testing.T) {
	input := studyPNG(t, 80, 40)
	output := studyPNG(t, 8, 4)
	for _, kind := range []string{"invalid bytes", "wrong MIME", "wrong dimensions", "wrong profile", "false preservation", "transformation disabled", "empty"} {
		t.Run(kind, func(t *testing.T) {
			c := fixtureConverter(t, func(w http.ResponseWriter, r *http.Request, data []byte) {
				config, _, _ := image.DecodeConfig(bytes.NewReader(output))
				w.Header().Set("Content-Type", "image/png")
				w.Header().Set("X-Image-Profile", studyProfile)
				w.Header().Set("X-Image-Preserved", "false")
				w.Header().Set("X-Image-Width", strconv.Itoa(config.Width))
				w.Header().Set("X-Image-Height", strconv.Itoa(config.Height))
				body := output
				switch kind {
				case "invalid bytes":
					body = []byte("not an image")
				case "wrong MIME":
					w.Header().Set("Content-Type", "image/jpeg")
				case "wrong dimensions":
					w.Header().Set("X-Image-Width", "100")
				case "wrong profile":
					w.Header().Set("X-Image-Profile", "avatar")
				case "false preservation":
					w.Header().Set("X-Image-Preserved", "true")
				case "empty":
					body = nil
				}
				w.Write(body)
			})
			if kind == "transformation disabled" {
				c.AllowStudyTransformation = false
			}
			store, err := New(Config{Directory: privateStudyDirectory(t), Converter: c})
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.Save(context.Background(), "study.png", bytes.NewReader(input))
			var failure *api.Error
			if !errors.As(err, &failure) || failure.Status != 502 {
				t.Fatalf("invalid response accepted: %v", err)
			}
		})
	}
}

func TestConverterKeepsSmallerOriginalAndCancellation(t *testing.T) {
	input := studyPNG(t, 4, 4)
	output := studyPNG(t, 80, 40)
	c := fixtureConverter(t, func(w http.ResponseWriter, r *http.Request, data []byte) {
		writeConverted(w, output, "image/png", false)
	})
	store, err := New(Config{Directory: privateStudyDirectory(t), Converter: c})
	if err != nil {
		t.Fatal(err)
	}
	actual, _, _, err := store.convertStudy(context.Background(), input, "study.png", "image/png")
	if err != nil || !bytes.Equal(actual, input) {
		t.Fatal("larger converted result replaced original")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := store.convertStudy(ctx, input, "study.png", "image/png"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
	blocking := fixtureConverter(t, func(w http.ResponseWriter, r *http.Request, data []byte) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})
	store, err = New(Config{Directory: privateStudyDirectory(t), Converter: blocking})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, _, err := store.convertStudy(ctx, input, "study.png", "image/png"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline lost: %v", err)
	}
}

func TestConverterNeverFollowsRedirect(t *testing.T) {
	var called atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Store(true) }))
	defer destination.Close()
	c := fixtureConverter(t, func(w http.ResponseWriter, r *http.Request, data []byte) { http.Redirect(w, r, destination.URL, 307) })
	store, err := New(Config{Directory: privateStudyDirectory(t), Converter: c})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.convertStudy(context.Background(), studyPNG(t, 4, 4), "study.png", "image/png"); err == nil || called.Load() {
		t.Fatal("credential redirect was followed")
	}
}
