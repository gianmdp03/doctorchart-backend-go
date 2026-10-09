package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestS3MissingAndUnavailableAreDistinct(t *testing.T) {
	for _, status := range []int{404, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			store, err := New(Config{Type: "s3", Endpoint: server.URL, Bucket: "owned-fixture", AccessKey: "owned-access", SecretKey: "owned-secret"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			_, _, err = store.Open(ctx, "12345678-1234-4123-8123-123456789012")
			var failure *api.Error
			if !errors.As(err, &failure) || failure.Status != status {
				t.Fatalf("status=%d error=%v", status, err)
			}
			if status == 503 {
				var picture bytes.Buffer
				if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				defer cancel()
				_, err = store.Save(ctx, "owned.png", bytes.NewReader(picture.Bytes()))
				if !errors.As(err, &failure) || failure.Status != 503 {
					t.Fatalf("Upload failure exposed internal error: %v", err)
				}
			}
		})
	}
}

func TestS3PrivateObjectProtocol(t *testing.T) {
	var mu sync.Mutex
	objects := map[string][]byte{}
	signed := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		signed = signed && strings.Contains(r.Header.Get("Authorization"), "Credential=fixture-access/")
		key := strings.TrimPrefix(r.URL.Path, "/fixture-bucket/")
		if r.Method == "GET" && r.URL.Query().Get("list-type") == "2" {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, "<ListBucketResult><Name>fixture-bucket</Name><IsTruncated>false</IsTruncated>")
			for key, value := range objects {
				fmt.Fprintf(w, "<Contents><Key>%s</Key><LastModified>2026-01-01T00:00:00Z</LastModified><Size>%d</Size></Contents>", key, len(value))
			}
			fmt.Fprint(w, "</ListBucketResult>")
			return
		}
		switch r.Method {
		case "PUT":
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			objects[key] = data
			w.Header().Set("ETag", "\"fixture\"")
		case "HEAD", "GET":
			data, ok := objects[key]
			if !ok {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.Header().Set("Content-Type", "image/png")
			if r.Method == "GET" {
				w.Write(data)
			}
		case "DELETE":
			delete(objects, key)
			w.WriteHeader(204)
		default:
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	store, err := New(Config{Type: "s3", Endpoint: server.URL, Bucket: "fixture-bucket", AccessKey: "fixture-access", SecretKey: "fixture-secret", Region: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(context.Background(), "a.png", bytes.NewReader(picture.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	reader, size, err := store.Open(context.Background(), saved.Key)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || size != int64(picture.Len()) || !bytes.Equal(actual, picture.Bytes()) {
		t.Fatal("S3 object bytes changed")
	}
	keys := []string{}
	if err := store.Candidates(context.Background(), time.Now(), func(key string) error { keys = append(keys, key); return nil }); err != nil || len(keys) != 1 || keys[0] != saved.Key {
		t.Fatalf("%v %v", keys, err)
	}
	if err := store.Delete(context.Background(), saved.Key); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Open(context.Background(), saved.Key); err == nil {
		t.Fatal("missing S3 object returned")
	}
	if !signed {
		t.Fatal("S3 request was not signed with the configured static credentials")
	}
}

func TestS3CandidatesIncludeEveryPage(t *testing.T) {
	keys := []string{"12345678-1234-4123-8123-123456789012", "87654321-1234-4123-8123-123456789012"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("continuation-token") == "second" {
			fmt.Fprintf(w, "<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>%s</Key><LastModified>2020-01-01T00:00:00Z</LastModified></Contents></ListBucketResult>", keys[1])
		} else {
			fmt.Fprintf(w, "<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>second</NextContinuationToken><Contents><Key>%s</Key><LastModified>2020-01-01T00:00:00Z</LastModified></Contents></ListBucketResult>", keys[0])
		}
	}))
	defer server.Close()
	store, err := New(Config{Type: "s3", Endpoint: server.URL, Bucket: "owned-fixture", AccessKey: "owned-access", SecretKey: "owned-secret"})
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	err = store.Candidates(context.Background(), time.Now().Add(-24*time.Hour), func(key string) error { found = append(found, key); return nil })
	if err != nil || len(found) != 2 || found[0] != keys[0] || found[1] != keys[1] {
		t.Fatalf("Missing orphan candidates on later S3 pages: %v, %v", found, err)
	}
}
