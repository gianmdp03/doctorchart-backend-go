package storage

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type unreadUpload struct{ reads atomic.Int32 }

func (r *unreadUpload) Read([]byte) (int, error) {
	r.reads.Add(1)
	return 0, errors.New("unexpected upload read")
}

func TestUploadsCanWaitForConverterWithoutHoldingValidationSlots(t *testing.T) {
	arrivals := make(chan struct{}, 21)
	proceed := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(proceed) }) }
	t.Cleanup(unblock)
	converter := fixtureConverter(t, func(w http.ResponseWriter, r *http.Request, data []byte) {
		arrivals <- struct{}{}
		<-proceed
		writeConverted(w, data, "image/png", true)
	})
	directory := privateStudyDirectory(t)
	store, err := New(Config{Directory: directory, Converter: converter})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results := make(chan error, 21)
	data := studyPNG(t, 4, 4)
	for i := 0; i < 21; i++ {
		go func() { _, err := store.Save(ctx, "study.png", bytes.NewReader(data)); results <- err }()
	}
	for i := 0; i < 21; i++ {
		select {
		case <-arrivals:
		case <-ctx.Done():
			t.Fatal("uploads could not join the shared queue")
		}
	}
	if len(store.slots) != 0 {
		t.Fatal("network wait kept validation resources occupied")
	}
	input := &unreadUpload{}
	_, err = store.Save(ctx, "study.png", input)
	var failure *api.Error
	if !errors.As(err, &failure) || failure.Status != 503 || input.reads.Load() != 0 {
		t.Fatal("upload capacity was not bounded before reading", err)
	}
	unblock()
	for i := 0; i < 21; i++ {
		if err := <-results; err != nil {
			t.Fatal("queued upload failed", err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 21 || len(store.uploads) != 0 || len(store.slots) != 0 {
		t.Fatal("queued storage mismatch or resource leak")
	}
}

func TestCancelledValidationWaitDoesNotReadOriginal(t *testing.T) {
	store, err := New(Config{Directory: privateStudyDirectory(t)})
	if err != nil {
		t.Fatal(err)
	}
	release1, err := store.acquireValidation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release1()
	release2, err := store.acquireValidation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release2()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := &unreadUpload{}
	result := make(chan error, 1)
	go func() { _, err := store.Save(ctx, "study.png", input); result <- err }()
	deadline := time.Now().Add(time.Second)
	for len(store.uploads) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(store.uploads) != 1 {
		t.Fatal("upload did not wait for validation")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || input.reads.Load() != 0 || len(store.uploads) != 0 {
			t.Fatal("cancelled validation leaked admission or read bytes", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled validation kept waiting")
	}
}
