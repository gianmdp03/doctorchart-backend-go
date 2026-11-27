package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

const (
	maxSignaturesPerWorkspace = 20
	maxSignaturePNGBytes      = 204800
	maxSignatureWidth         = 1600
	maxSignatureHeight        = 800
	minSignatureInkPixels     = 12
)

// SignatureSummary represents the metadata of a doctor's saved handwritten signature.
type SignatureSummary struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	StampText  string `json:"stampText"`
	IsFavorite bool   `json:"isFavorite"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	SHA256     string `json:"sha256"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

// SignatureAsset holds the verified PNG bytes and metadata used during PDF rendering.
type SignatureAsset struct {
	ID        int64
	Name      string
	StampText string
	ImagePNG  []byte
	Width     int
	Height    int
	SHA256    string
	Favorite  bool
}

func decodeSignatureImageField(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, api.Failure(400, "INVALID_SIGNATURE_IMAGE", "La imagen de la firma es obligatoria.")
	}
	if strings.HasPrefix(trimmed, "data:") {
		const prefix = "data:image/png;base64,"
		if len(trimmed) < len(prefix) || !strings.EqualFold(trimmed[:len(prefix)], prefix) {
			return nil, api.Failure(400, "INVALID_SIGNATURE_IMAGE", "La firma debe enviarse en formato PNG.")
		}
		trimmed = trimmed[len(prefix):]
	}
	if len(trimmed) > base64.StdEncoding.EncodedLen(maxSignaturePNGBytes)+16 {
		return nil, api.Failure(413, "SIGNATURE_TOO_LARGE", "La firma supera el tamaño máximo permitido (200 KB).")
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, api.Failure(400, "INVALID_SIGNATURE_IMAGE", "La imagen de la firma no es válida.")
	}
	return decoded, nil
}

func sanitizeSignaturePNG(raw []byte) ([]byte, int, int, string, error) {
	if len(raw) == 0 {
		return nil, 0, 0, "", api.Failure(400, "INVALID_SIGNATURE_IMAGE", "La imagen de la firma está vacía.")
	}
	if len(raw) > maxSignaturePNGBytes {
		return nil, 0, 0, "", api.Failure(413, "SIGNATURE_TOO_LARGE", "La firma supera el tamaño máximo permitido (200 KB).")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, "", api.Failure(400, "INVALID_SIGNATURE_IMAGE", "La firma debe ser una imagen PNG válida.")
	}
	if cfg.Width < 1 || cfg.Width > maxSignatureWidth || cfg.Height < 1 || cfg.Height > maxSignatureHeight {
		return nil, 0, 0, "", api.Failure(400, "INVALID_SIGNATURE_DIMENSIONS", fmt.Sprintf("Las dimensiones de la firma deben estar entre 1x1 y %dx%d px.", maxSignatureWidth, maxSignatureHeight))
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, "", api.Failure(400, "INVALID_SIGNATURE_IMAGE", "No se pudo leer la imagen PNG de la firma.")
	}
	bounds := decoded.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w < 1 || w > maxSignatureWidth || h < 1 || h > maxSignatureHeight {
		return nil, 0, 0, "", api.Failure(400, "INVALID_SIGNATURE_DIMENSIONS", fmt.Sprintf("Las dimensiones de la firma deben estar entre 1x1 y %dx%d px.", maxSignatureWidth, maxSignatureHeight))
	}
	clean := image.NewNRGBA(image.Rect(0, 0, w, h))
	inkPixels := 0
	for y := range h {
		for x := range w {
			c := color.NRGBAModel.Convert(decoded.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			if c.A > 16 {
				inkPixels++
			}
			clean.SetNRGBA(x, y, c)
		}
	}
	if inkPixels < minSignatureInkPixels {
		return nil, 0, 0, "", api.Failure(400, "EMPTY_SIGNATURE", "Dibujá la firma antes de guardarla.")
	}
	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&buf, clean); err != nil {
		return nil, 0, 0, "", err
	}
	cleanBytes := buf.Bytes()
	if len(cleanBytes) > maxSignaturePNGBytes {
		return nil, 0, 0, "", api.Failure(413, "SIGNATURE_TOO_LARGE", "La firma supera el tamaño máximo permitido (200 KB).")
	}
	sum := sha256.Sum256(cleanBytes)
	return cleanBytes, w, h, fmt.Sprintf("%x", sum), nil
}

func scanSignatureSummary(row pgx.Row) (SignatureSummary, error) {
	var s SignatureSummary
	var createdAt, updatedAt time.Time
	err := row.Scan(&s.ID, &s.Name, &s.StampText, &s.IsFavorite, &s.Width, &s.Height, &s.SHA256, &createdAt, &updatedAt)
	if err != nil {
		return s, err
	}
	s.CreatedAt = api.FormatInstant(createdAt)
	s.UpdatedAt = api.FormatInstant(updatedAt)
	return s, nil
}

func normalizeSignatureName(request map[string]any) (string, error) {
	raw, err := api.Text(request, "name", 120)
	if err != nil {
		return "", api.Invalid("Indicá un nombre para la firma (hasta 120 caracteres).")
	}
	trimmed := api.JavaTrim(raw)
	if api.JavaBlank(trimmed) || api.JavaLength(trimmed) > 120 {
		return "", api.Invalid("Indicá un nombre para la firma (hasta 120 caracteres).")
	}
	return trimmed, nil
}

func normalizeStampText(request map[string]any) (string, error) {
	opt, err := api.OptionalText(request, "stampText")
	if err != nil {
		return "", err
	}
	if opt == nil {
		return "", nil
	}
	trimmed := api.JavaTrim(*opt)
	if api.JavaLength(trimmed) > 200 {
		return "", api.Invalid("La aclaración o sello admite hasta 200 caracteres.")
	}
	return trimmed, nil
}

// ListSignatures returns all saved signatures for the authenticated doctor's workspace.
func (s *Store) ListSignatures(ctx context.Context, userID string) ([]SignatureSummary, error) {
	items := []SignatureSummary{}
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT ds.id, ds.name, ds.stamp_text, ds.is_favorite, ds.image_width, ds.image_height, ds.sha256, ds.created_at, ds.updated_at
			FROM doctor_signatures ds
			JOIN doctor_workspaces w ON w.id = ds.workspace_id
			WHERE w.user_id = $1
			ORDER BY ds.is_favorite DESC, ds.id ASC`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanSignatureSummary(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

// SignatureImage returns the sanitized PNG bytes and SHA-256 digest for a doctor's signature.
func (s *Store) SignatureImage(ctx context.Context, userID string, id int64) ([]byte, string, error) {
	var pngBytes []byte
	var digest string
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT ds.image_png, ds.sha256
			FROM doctor_signatures ds
			JOIN doctor_workspaces w ON w.id = ds.workspace_id
			WHERE w.user_id = $1 AND ds.id = $2`, userID, id).Scan(&pngBytes, &digest)
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Failure(404, "SIGNATURE_NOT_FOUND", "No se encontró la firma solicitada.")
		}
		return err
	})
	return pngBytes, digest, err
}

// CreateSignature validates and stores a new handwritten signature for the doctor's workspace.
func (s *Store) CreateSignature(ctx context.Context, userID string, request map[string]any) (SignatureSummary, error) {
	var created SignatureSummary
	if err := api.Only(request, "name", "stampText", "imageBase64", "isFavorite"); err != nil {
		return created, err
	}
	name, err := normalizeSignatureName(request)
	if err != nil {
		return created, err
	}
	stampText, err := normalizeStampText(request)
	if err != nil {
		return created, err
	}
	rawImg, ok := request["imageBase64"].(string)
	if !ok {
		return created, api.Failure(400, "INVALID_SIGNATURE_IMAGE", "La imagen de la firma es obligatoria.")
	}
	decoded, err := decodeSignatureImageField(rawImg)
	if err != nil {
		return created, err
	}
	cleanPNG, width, height, digest, err := sanitizeSignaturePNG(decoded)
	if err != nil {
		return created, err
	}
	isFavorite := false
	if rawFav, present := request["isFavorite"]; present && rawFav != nil {
		fav, ok := rawFav.(bool)
		if !ok {
			return created, api.Invalid("El campo isFavorite debe ser booleano.")
		}
		isFavorite = fav
	}

	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		var workspaceID int64
		if err := tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1 FOR UPDATE", userID).Scan(&workspaceID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return api.Failure(409, "ONBOARDING_REQUIRED", "Completá la configuración inicial antes de guardar firmas.")
			}
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM doctor_signatures WHERE workspace_id=$1", workspaceID).Scan(&count); err != nil {
			return err
		}
		if count >= maxSignaturesPerWorkspace {
			return api.Failure(409, "SIGNATURE_LIMIT_REACHED", fmt.Sprintf("Podés guardar hasta %d firmas en tu cuenta.", maxSignaturesPerWorkspace))
		}
		if count == 0 {
			isFavorite = true
		}
		if isFavorite {
			if _, err := tx.Exec(ctx, `UPDATE doctor_signatures
				SET is_favorite = false, updated_at = greatest(clock_timestamp(), updated_at + interval '1 microsecond')
				WHERE workspace_id = $1 AND is_favorite = true`, workspaceID); err != nil {
				return err
			}
		}
		row := tx.QueryRow(ctx, `INSERT INTO doctor_signatures
			(workspace_id, name, stamp_text, image_png, image_width, image_height, sha256, is_favorite)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id, name, stamp_text, is_favorite, image_width, image_height, sha256, created_at, updated_at`,
			workspaceID, name, stampText, cleanPNG, width, height, digest, isFavorite)
		var scanErr error
		created, scanErr = scanSignatureSummary(row)
		return scanErr
	})
	return created, err
}

// UpdateSignature updates name, stamp text, drawing, or favorite status of an existing signature.
func (s *Store) UpdateSignature(ctx context.Context, userID string, id int64, request map[string]any) (SignatureSummary, error) {
	var updated SignatureSummary
	if err := api.Only(request, "name", "stampText", "imageBase64", "isFavorite"); err != nil {
		return updated, err
	}
	if len(request) == 0 {
		return updated, api.Invalid("Indicá al menos un cambio para actualizar la firma.")
	}
	var newName *string
	if _, present := request["name"]; present {
		name, err := normalizeSignatureName(request)
		if err != nil {
			return updated, err
		}
		newName = &name
	}
	var newStamp *string
	if _, present := request["stampText"]; present {
		stamp, err := normalizeStampText(request)
		if err != nil {
			return updated, err
		}
		newStamp = &stamp
	}
	var newPNG []byte
	var newWidth, newHeight int
	var newDigest string
	if rawImgVal, present := request["imageBase64"]; present && rawImgVal != nil {
		rawImg, ok := rawImgVal.(string)
		if !ok {
			return updated, api.Failure(400, "INVALID_SIGNATURE_IMAGE", "La imagen de la firma no es válida.")
		}
		decoded, err := decodeSignatureImageField(rawImg)
		if err != nil {
			return updated, err
		}
		newPNG, newWidth, newHeight, newDigest, err = sanitizeSignaturePNG(decoded)
		if err != nil {
			return updated, err
		}
	}
	var newFav *bool
	if rawFav, present := request["isFavorite"]; present {
		fav, ok := rawFav.(bool)
		if !ok {
			return updated, api.Invalid("El campo isFavorite debe ser booleano.")
		}
		newFav = &fav
	}

	err := s.transaction(ctx, false, func(tx pgx.Tx) error {
		var workspaceID int64
		if err := tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1 FOR UPDATE", userID).Scan(&workspaceID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return api.Failure(404, "SIGNATURE_NOT_FOUND", "No se encontró la firma solicitada.")
			}
			return err
		}
		var curName, curStamp, curDigest string
		var curFav bool
		var curPNG []byte
		var curWidth, curHeight int
		err := tx.QueryRow(ctx, `SELECT name, stamp_text, image_png, image_width, image_height, sha256, is_favorite
			FROM doctor_signatures WHERE id = $1 AND workspace_id = $2`, id, workspaceID).
			Scan(&curName, &curStamp, &curPNG, &curWidth, &curHeight, &curDigest, &curFav)
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Failure(404, "SIGNATURE_NOT_FOUND", "No se encontró la firma solicitada.")
		}
		if err != nil {
			return err
		}
		if newName != nil {
			curName = *newName
		}
		if newStamp != nil {
			curStamp = *newStamp
		}
		if len(newPNG) > 0 {
			curPNG, curWidth, curHeight, curDigest = newPNG, newWidth, newHeight, newDigest
		}
		if newFav != nil {
			if *newFav && !curFav {
				if _, err := tx.Exec(ctx, `UPDATE doctor_signatures
					SET is_favorite = false, updated_at = greatest(clock_timestamp(), updated_at + interval '1 microsecond')
					WHERE workspace_id = $1 AND is_favorite = true AND id <> $2`, workspaceID, id); err != nil {
					return err
				}
			}
			curFav = *newFav
		}
		row := tx.QueryRow(ctx, `UPDATE doctor_signatures
			SET name = $3, stamp_text = $4, image_png = $5, image_width = $6, image_height = $7, sha256 = $8, is_favorite = $9,
			    updated_at = greatest(clock_timestamp(), updated_at + interval '1 microsecond')
			WHERE id = $1 AND workspace_id = $2
			RETURNING id, name, stamp_text, is_favorite, image_width, image_height, sha256, created_at, updated_at`,
			id, workspaceID, curName, curStamp, curPNG, curWidth, curHeight, curDigest, curFav)
		var scanErr error
		updated, scanErr = scanSignatureSummary(row)
		return scanErr
	})
	return updated, err
}

// DeleteSignature removes a saved signature and promotes the most recent remaining one if it was favorite.
func (s *Store) DeleteSignature(ctx context.Context, userID string, id int64) error {
	return s.transaction(ctx, false, func(tx pgx.Tx) error {
		var workspaceID int64
		if err := tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1 FOR UPDATE", userID).Scan(&workspaceID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return api.Failure(404, "SIGNATURE_NOT_FOUND", "No se encontró la firma solicitada.")
			}
			return err
		}
		var wasFavorite bool
		err := tx.QueryRow(ctx, "DELETE FROM doctor_signatures WHERE id = $1 AND workspace_id = $2 RETURNING is_favorite", id, workspaceID).Scan(&wasFavorite)
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Failure(404, "SIGNATURE_NOT_FOUND", "No se encontró la firma solicitada.")
		}
		if err != nil {
			return err
		}
		if wasFavorite {
			if _, err := tx.Exec(ctx, `UPDATE doctor_signatures
				SET is_favorite = true, updated_at = greatest(clock_timestamp(), updated_at + interval '1 microsecond')
				WHERE id = (SELECT id FROM doctor_signatures WHERE workspace_id = $1 ORDER BY updated_at DESC, id DESC LIMIT 1)`,
				workspaceID); err != nil {
				return err
			}
		}
		return nil
	})
}

// ResolveSignature resolves the signature to embed in a clinical PDF within an existing transaction.
func (s *Store) ResolveSignature(ctx context.Context, tx pgx.Tx, userID, mode string, signatureID *int64) (*SignatureAsset, error) {
	if mode == "NONE" {
		return nil, nil
	}
	var asset SignatureAsset
	var row pgx.Row
	if signatureID != nil {
		row = tx.QueryRow(ctx, `SELECT ds.id, ds.name, ds.stamp_text, ds.image_png, ds.image_width, ds.image_height, ds.sha256, ds.is_favorite
			FROM doctor_signatures ds
			JOIN doctor_workspaces w ON w.id = ds.workspace_id
			WHERE w.user_id = $1 AND ds.id = $2`, userID, *signatureID)
	} else {
		row = tx.QueryRow(ctx, `SELECT ds.id, ds.name, ds.stamp_text, ds.image_png, ds.image_width, ds.image_height, ds.sha256, ds.is_favorite
			FROM doctor_signatures ds
			JOIN doctor_workspaces w ON w.id = ds.workspace_id
			WHERE w.user_id = $1 AND ds.is_favorite = true
			LIMIT 1`, userID)
	}
	err := row.Scan(&asset.ID, &asset.Name, &asset.StampText, &asset.ImagePNG, &asset.Width, &asset.Height, &asset.SHA256, &asset.Favorite)
	if errors.Is(err, pgx.ErrNoRows) {
		if signatureID != nil {
			return nil, api.Failure(404, "SIGNATURE_NOT_FOUND", "La firma seleccionada no está disponible.")
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}
