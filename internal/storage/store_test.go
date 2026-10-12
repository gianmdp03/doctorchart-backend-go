package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrivateStudyRoundTripAndDeletion(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := New(Config{Directory: directory, Converter: preservingConverter(t)})
	if err != nil {
		t.Fatal(err)
	}
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: 100, A: 255})
	var content bytes.Buffer
	if err := png.Encode(&content, picture); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(context.Background(), "estudio.png", bytes.NewReader(content.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if saved.ContentType != "image/png" || saved.Size != int64(content.Len()) || !storageKey.MatchString(saved.Key) {
		t.Fatalf("%+v", saved)
	}
	file, size, err := store.Open(context.Background(), saved.Key)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(file)
	closeErr := file.Close()
	if err != nil || closeErr != nil || size != saved.Size || !bytes.Equal(actual, content.Bytes()) {
		t.Fatal("small medical image bytes changed")
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(filepath.Join(directory, saved.Key), old, old); err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	if err := store.Candidates(context.Background(), time.Now().Add(-24*time.Hour), func(key string) error { keys = append(keys, key); return nil }); err != nil || len(keys) != 1 {
		t.Fatalf("%v %v", keys, err)
	}
	if err := store.Delete(context.Background(), saved.Key); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), saved.Key); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Open(context.Background(), saved.Key); err == nil {
		t.Fatal("deleted attachment remained accessible")
	}
	if _, _, err := store.Open(context.Background(), "../estudio.png"); err == nil {
		t.Fatal("path traversal accepted")
	}
}
func TestStudyStoresRemotelyProcessedJPEGAndMetadata(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	var processed bytes.Buffer
	if err := jpeg.Encode(&processed, image.NewRGBA(image.Rect(0, 0, 682, 2048)), &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	converter := fixtureConverter(t, func(w http.ResponseWriter, r *http.Request, data []byte) {
		writeConverted(w, processed.Bytes(), "image/jpeg", false)
	})
	store, err := New(Config{Directory: directory, Converter: converter})
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 3000, 1000)), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	exif := []byte("Exif\x00\x00II*\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00\x06\x00\x00\x00\x00\x00\x00\x00")
	segment := []byte{255, 225, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(exif)+2))
	original := append(append(append([]byte{}, buffer.Bytes()[:2]...), segment...), exif...)
	original = append(original, buffer.Bytes()[2:]...)
	saved, err := store.Save(context.Background(), "original.jpg", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	file, _, err := store.Open(context.Background(), saved.Key)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(file)
	closeErr := file.Close()
	if err != nil || closeErr != nil || len(actual) >= len(original) || saved.Size != int64(len(actual)) {
		t.Fatal("JPEG was not compressed or stored size is incorrect")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(actual))
	if err != nil || format != "jpeg" || config.Width != 682 || config.Height != 2048 || !bytes.Equal(actual, processed.Bytes()) {
		t.Fatalf("orientation or dimensions incorrect: %+v %s %v", config, format, err)
	}
}

func TestStudyRejectsOversizedResolutionWithoutDecodingOrResampling(t *testing.T) {
	var content bytes.Buffer
	if err := png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := content.Bytes()
	binary.BigEndian.PutUint32(data[16:20], 10000)
	binary.BigEndian.PutUint32(data[20:24], 5000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, err := inspect(context.Background(), data, "original.png"); err == nil {
		t.Fatal("Unsafe decompression dimensions accepted")
	}
}

func TestStudyContentValidation(t *testing.T) {
	for _, name := range []string{"../a.pdf", "a\\b.png", "a:pdf", "a\n.png", "a\u202e.png", ""} {
		if validateName(name) == nil {
			t.Fatalf("unsafe name %q", name)
		}
	}
	if _, err := inspect(context.Background(), []byte("%PDF-1.7\nnot a document"), "a.pdf"); err == nil {
		t.Fatal("malformed PDF accepted")
	}
	var content bytes.Buffer
	if err := png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := inspect(context.Background(), content.Bytes(), "a.jpg"); err == nil {
		t.Fatal("image extension mismatch accepted")
	}
	if _, err := inspect(context.Background(), content.Bytes(), "a.PNG"); err != nil {
		t.Fatal(err)
	}
}
func TestStudySymbolicLinkRejected(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := New(Config{Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(other, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := newKey()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, key)
	if err := os.Symlink(other, link); err != nil {
		t.Skip("symbolic links unavailable on this host")
	}
	if _, _, err := store.Open(context.Background(), key); err == nil {
		t.Fatal("symbolic attachment opened")
	}
	if err := store.Delete(context.Background(), key); err == nil {
		t.Fatal("symbolic attachment deleted")
	}
	data, err := os.ReadFile(other)
	if err != nil || string(data) != "unrelated" {
		t.Fatal("unrelated file was changed")
	}
}
