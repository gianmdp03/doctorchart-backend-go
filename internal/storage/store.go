// Package storage owns private study attachments; database deletion intents are owned by clinical transactions.
package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	common "github.com/bycode/bycode-image-converter-go/pkg/media"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

// MaxBytes bounds original uploads: 10 MB in decimal units.
const MaxBytes = 10_000_000

var storageKey = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type Config struct {
	Managed                                                         *common.Config
	Type, Directory, Endpoint, Bucket, AccessKey, SecretKey, Region string
	Converter                                                       ConverterConfig
}
type Store struct {
	library   *common.Library
	root      string
	client    *s3.Client
	bucket    string
	slots     chan struct{}
	uploads   chan struct{}
	converter *studyConverter
}
type File struct {
	Asset                  *common.Asset
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
	return api.Failure(413, "STUDY_FILE_TOO_LARGE", "El archivo supera el tamaño máximo permitido de 10 MB.")
}
func New(config Config) (*Store, error) {
	store := &Store{slots: make(chan struct{}, 2), uploads: make(chan struct{}, 21)}
	var err error
	store.converter, err = newStudyConverter(config.Converter)
	if err != nil {
		return nil, err
	}
	if config.Managed != nil {
		manager, err := common.NewManager(*config.Managed)
		if err != nil {
			return nil, err
		}
		store.library = &common.Library{Manager: manager}
		return store, nil
	}
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
	upload, err := s.BeginUpload(ctx)
	if err != nil {
		return File{}, err
	}
	defer upload.Close()
	return upload.Save(ctx, name, input)
}

// Upload reserves admission before the HTTP handler reads/spools multipart data.
// Its owner closes it only after Save has finished (or parsing fails).
type Upload struct {
	store  *Store
	closed atomic.Bool
}

func (s *Store) BeginUpload(ctx context.Context) (*Upload, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case s.uploads <- struct{}{}:
		return &Upload{store: s}, nil
	default:
		return nil, api.Failure(503, "STUDY_VALIDATION_BUSY", "La cola de cargas está completa. Reintentá en unos segundos.")
	}
}

func (u *Upload) Close() {
	if !u.closed.Swap(true) {
		<-u.store.uploads
	}
}

func (u *Upload) Save(ctx context.Context, name string, input io.Reader) (File, error) {
	if u.closed.Load() {
		return File{}, unavailable()
	}
	return u.store.save(ctx, name, input)
}

func (s *Store) save(ctx context.Context, name string, input io.Reader) (File, error) {
	content, contentType, err := s.readOriginal(ctx, name, input)
	if err != nil {
		return File{}, err
	}
	content, name, contentType, err = s.convertStudy(ctx, content, name, contentType)
	if err != nil {
		return File{}, err
	}
	key, err := newKey()
	if err != nil {
		return File{}, unavailable()
	}
	stored := File{Key: key, Name: name, ContentType: contentType, Size: int64(len(content))}
	if s.library != nil {
		identity, _ := ctx.Value(mediaIdentityKey{}).(mediaIdentity)
		asset, err := s.library.Manager.Prepare(ctx, key, identity.owner, "studies", "original", contentType, "private", "doctorchart-study", identity.refs, content)
		if err != nil {
			return File{}, unavailable()
		}
		stored.Asset = &asset
		return stored, nil
	}
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

// Decode at most two originals/responses at a time. Release this resource
// before waiting on the shared converter so uploads can join its queue.
func (s *Store) acquireValidation(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Store) readOriginal(ctx context.Context, name string, input io.Reader) ([]byte, string, error) {
	release, err := s.acquireValidation(ctx)
	if err != nil {
		return nil, "", err
	}
	defer release()
	content, err := io.ReadAll(io.LimitReader(input, MaxBytes+1))
	if err != nil {
		return nil, "", unavailable()
	}
	if len(content) == 0 {
		return nil, "", api.Failure(400, "EMPTY_STUDY_FILE", "Seleccioná un archivo que no esté vacío.")
	}
	if len(content) > MaxBytes {
		return nil, "", TooLarge()
	}
	if err := validateName(name); err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	contentType, err := inspect(ctx, content, name)
	return content, contentType, err
}
func (s *Store) Open(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if s.library != nil {
		asset, err := s.library.Catalog.Get(ctx, key)
		if err != nil {
			return nil, 0, mediaReadError(err)
		}
		f, err := s.library.Manager.Open(ctx, asset)
		return f, asset.Size, mediaReadError(err)
	}
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
	if s.library != nil {
		return s.library.Retire(ctx, key)
	}
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
	// Managed inventories belong to the manual, read-only reconciler.
	if s.library != nil {
		return nil
	}
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

type mediaIdentityKey struct{}
type mediaIdentity struct {
	owner string
	refs  []common.Association
}

func WithOwner(ctx context.Context, owner, patientIdentity string) context.Context {
	return context.WithValue(ctx, mediaIdentityKey{}, mediaIdentity{owner, []common.Association{{EntityType: "patient", EntityID: patientIdentity, Role: "study"}}})
}
func (s *Store) Managed() bool { return s.library != nil }
func (s *Store) SetCatalog(db *sql.DB) {
	if s.library != nil {
		s.library.Catalog = common.Catalog{DB: db}
	}
}
func (s *Store) Flush(ctx context.Context) error {
	if s.library == nil {
		return nil
	}
	return s.library.Flush(ctx)
}
func (s *Store) RecoveryMode() bool {
	return s.library != nil && s.library.Manager.Config.Policy.RecoveryMode
}
func (s *Store) PruneRecovery(ctx context.Context) error {
	if s.library == nil {
		return nil
	}
	return s.library.PruneRecovery(ctx)
}
func mediaReadError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, common.ErrMissing) {
		return Missing()
	}
	return unavailable()
}
