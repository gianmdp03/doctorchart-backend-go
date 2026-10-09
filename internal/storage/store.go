// Package storage owns private study attachments; database deletion intents are owned by clinical transactions.
package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

const MaxBytes = 25 * 1024 * 1024

var storageKey = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type Config struct{ Type, Directory, Endpoint, Bucket, AccessKey, SecretKey, Region string }
type Store struct {
	root   string
	client *s3.Client
	bucket string
	slots  chan struct{}
}
type File struct {
	Key, Name, ContentType string
	Size                   int64
}

func unavailable() error {
	return api.Failure(503, "STUDY_STORAGE_UNAVAILABLE", "No se pudo guardar el archivo. Reintentá más tarde.")
}
func Missing() error { return api.Failure(404, "RESOURCE_NOT_FOUND", "No se encontró el recurso.") }
func readFailure(err error) error {
	var response interface{ HTTPStatusCode() int }
	if errors.As(err, &response) && response.HTTPStatusCode() == http.StatusNotFound {
		return Missing()
	}
	return unavailable()
}
func TooLarge() error {
	return api.Failure(413, "STUDY_FILE_TOO_LARGE", "El archivo supera el tamaño máximo permitido de 25 MB.")
}
func New(config Config) (*Store, error) {
	store := &Store{slots: make(chan struct{}, 2)}
	if config.Type == "s3" {
		if api.JavaBlank(config.Bucket) {
			return nil, errors.New("app.storage.s3.bucket must be configured when storage type is s3")
		}
		region := config.Region
		if api.JavaBlank(region) {
			region = "auto"
		}
		settings := aws.Config{Region: region, Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.AccessKey, config.SecretKey, "")), HTTPClient: &http.Client{Timeout: 60 * time.Second}}
		store.bucket = config.Bucket
		store.client = s3.NewFromConfig(settings, func(options *s3.Options) {
			if !api.JavaBlank(config.Endpoint) {
				options.BaseEndpoint = &config.Endpoint
			}
		})
		return store, nil
	}
	if config.Type != "" && config.Type != "local" {
		return nil, errors.New("invalid app.storage.type")
	}
	directory := filepath.Clean(config.Directory)
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if api.JavaBlank(config.Directory) || !filepath.IsAbs(directory) || filepath.Dir(directory) == directory || within(cwd, directory) {
		return nil, errors.New("app.storage.studies-dir debe ser una ruta absoluta, privada y escribible fuera del repositorio, sin enlaces simbólicos.")
	}
	if err := checkAncestors(directory); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	if err := checkAncestors(directory); err != nil {
		return nil, err
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return nil, errors.New("invalid private storage directory")
	}
	if os.PathSeparator != '\\' && info.Mode().Perm() != 0700 {
		return nil, errors.New("storage directory must have mode 0700")
	}
	probe, err := os.CreateTemp(directory, ".storage-permission-")
	if err != nil {
		return nil, errors.New("storage directory is not writable")
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		return nil, err
	}
	if err := os.Remove(name); err != nil {
		return nil, err
	}
	store.root = directory
	return store, nil
}
func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
}
func checkAncestors(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("storage symbolic links are forbidden")
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}
func validateKey(key string) error {
	if !storageKey.MatchString(key) {
		return api.Failure(409, "INVALID_STUDY_STORAGE_KEY", "El archivo del estudio no está disponible.")
	}
	return nil
}
func newKey() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = value[6]&15 | 64
	value[8] = value[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}
func (s *Store) Save(ctx context.Context, name string, input io.Reader) (File, error) {
	content, err := io.ReadAll(io.LimitReader(input, MaxBytes+1))
	if err != nil {
		return File{}, unavailable()
	}
	if len(content) == 0 {
		return File{}, api.Failure(400, "EMPTY_STUDY_FILE", "Seleccioná un archivo que no esté vacío.")
	}
	if len(content) > MaxBytes {
		return File{}, TooLarge()
	}
	if err := validateName(name); err != nil {
		return File{}, err
	}
	// Match the original's two decoding slots; cancellation never leaves a detached decoder.
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return File{}, api.Failure(503, "STUDY_VALIDATION_BUSY", "Hay otras cargas en proceso. Reintentá en unos segundos.")
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	// Studies may contain diagnostic material. Preserve the original bytes and
	// EXIF metadata; reject unsafe inputs instead of silently resampling them.
	contentType, err := inspect(ctx, content, name)
	if err != nil {
		return File{}, err
	}
	key, err := newKey()
	if err != nil {
		return File{}, unavailable()
	}
	stored := File{key, name, contentType, int64(len(content))}
	if s.client != nil {
		_, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: &s.bucket, Key: &key, ContentType: &contentType, ContentLength: &stored.Size, Body: bytes.NewReader(content)})
		if err != nil {
			return File{}, unavailable()
		}
		return stored, nil
	}
	if err := checkAncestors(s.root); err != nil {
		return File{}, unavailable()
	}
	file, err := os.OpenFile(filepath.Join(s.root, key), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return File{}, unavailable()
	}
	_, writeErr := file.Write(content)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = s.Delete(ctx, key)
		return File{}, unavailable()
	}
	return stored, nil
}
func (s *Store) Open(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if err := validateKey(key); err != nil {
		return nil, 0, err
	}
	if s.client != nil {
		head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
		if err != nil {
			return nil, 0, readFailure(err)
		}
		object, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
		if err != nil {
			return nil, 0, readFailure(err)
		}
		return object.Body, aws.ToInt64(head.ContentLength), nil
	}
	if err := checkAncestors(s.root); err != nil {
		return nil, 0, Missing()
	}
	target := filepath.Join(s.root, key)
	info, err := os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() {
		return nil, 0, Missing()
	}
	// OpenRoot prevents a swapped parent link from escaping the private directory.
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, 0, Missing()
	}
	defer root.Close()
	file, err := root.Open(key)
	if err != nil {
		return nil, 0, Missing()
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		file.Close()
		return nil, 0, Missing()
	}
	return file, opened.Size(), nil
}
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if s.client != nil {
		_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key})
		return err
	}
	if err := checkAncestors(s.root); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Join(s.root, key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("invalid storage object")
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return err
	}
	defer root.Close()
	err = root.Remove(key)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (s *Store) Candidates(ctx context.Context, cutoff time.Time, visit func(string) error) error {
	if s.client != nil {
		pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: &s.bucket}, func(options *s3.ListObjectsV2PaginatorOptions) {
			options.StopOnDuplicateToken = true
		})
		for pages.HasMorePages() {
			response, err := pages.NextPage(ctx)
			if err != nil {
				return unavailable()
			}
			for _, object := range response.Contents {
				key := aws.ToString(object.Key)
				if storageKey.MatchString(key) && object.LastModified != nil && object.LastModified.Before(cutoff) {
					if err := visit(key); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if err := checkAncestors(s.root); err != nil {
		return err
	}
	directory, err := os.Open(s.root)
	if err != nil {
		return err
	}
	defer directory.Close()
	for {
		entries, err := directory.ReadDir(100)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		for _, entry := range entries {
			if !storageKey.MatchString(entry.Name()) {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
				continue
			}
			if err := visit(entry.Name()); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}
