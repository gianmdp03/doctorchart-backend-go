package growth

import (
	"bytes"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func windowsReferenceFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	source := fstest.MapFS{}
	err := fs.WalkDir(files, "data", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		payload, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		payload = bytes.ReplaceAll(payload, []byte("\r\n"), []byte("\n"))
		payload = bytes.ReplaceAll(payload, []byte("\n"), []byte("\r\n"))
		source[path] = &fstest.MapFile{Data: payload}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestLoadWindowsLineEndings(t *testing.T) {
	want := references(t)
	got, err := loadReferences(windowsReferenceFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want.tables, got.tables) {
		t.Fatal("Windows line endings changed WHO reference values")
	}
}

func TestLoadWindowsLineEndingsRejectsChangedData(t *testing.T) {
	source := windowsReferenceFiles(t)
	file := source["data/wfl06_girls.csv"]
	// Alter the first numeric row while retaining the Windows line endings.
	index := bytes.Index(file.Data, []byte("\r\n")) + 2
	file.Data[index] = '9'
	_, err := loadReferences(source)
	if err == nil || !strings.Contains(err.Error(), "WHO wfl06_girls: normalized checksum mismatch") {
		t.Fatalf("expected integrity rejection, got %v", err)
	}
}
