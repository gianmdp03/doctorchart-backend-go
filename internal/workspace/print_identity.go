package workspace

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
)

const maxLogoUploadBytes = 10_000_000

// LogoConverter compresses and validates clinic logos through the shared media converter.
type LogoConverter interface {
	ConvertClinicLogo(ctx context.Context, data []byte) (storage.ConvertedLogo, error)
}

// PrintIdentity exposes the doctor's configurable print license and clinic logo status.
type PrintIdentity struct {
	ProfessionalLicense string  `json:"professionalLicense"`
	LicenseConfigured   bool    `json:"licenseConfigured"`
	HasLogo             bool    `json:"hasLogo"`
	LogoSHA256          *string `json:"logoSha256"`
	LogoWidth           *int    `json:"logoWidth"`
	LogoHeight          *int    `json:"logoHeight"`
	UpdatedAt           *string `json:"updatedAt"`
}

// LogoAsset holds the verified PNG bytes and metadata used when rendering PDFs.
type LogoAsset struct {
	PNG    []byte
	Width  int
	Height int
	SHA256 string
}

func normalizeProfessionalLicense(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", api.Invalid("La matrícula profesional contiene caracteres inválidos.")
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029 {
			return "", api.Invalid("La matrícula profesional no admite caracteres de control.")
		}
	}
	trimmed := api.JavaTrim(raw)
	if api.JavaLength(trimmed) > 80 {
		return "", api.Invalid("La matrícula profesional admite hasta 80 caracteres.")
	}
	return trimmed, nil
}

func ensureWorkspaceRow(ctx context.Context, tx pgx.Tx, userID string) (int64, error) {
	var workspaceID int64
	err := tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1 FOR UPDATE", userID).Scan(&workspaceID)
	if err == nil {
		return workspaceID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO doctor_workspaces(user_id,version,created_at,updated_at)
		VALUES($1,0,clock_timestamp(),clock_timestamp()) RETURNING id`, userID).Scan(&workspaceID)
	return workspaceID, err
}

func readPrintIdentityTx(ctx context.Context, tx pgx.Tx, userID string) (PrintIdentity, error) {
	var result PrintIdentity
	var sha *string
	var width, height *int
	var updatedAt *string
	err := tx.QueryRow(ctx, `SELECT w.professional_license, w.license_configured,
		l.sha256, l.image_width, l.image_height,
		CASE WHEN l.updated_at IS NOT NULL THEN to_char(l.updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') END
		FROM doctor_workspaces w
		LEFT JOIN doctor_clinic_logos l ON l.workspace_id = w.id
		WHERE w.user_id = $1`, userID).Scan(&result.ProfessionalLicense, &result.LicenseConfigured, &sha, &width, &height, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PrintIdentity{}, nil
	}
	if err != nil {
		return result, err
	}
	if sha != nil {
		result.HasLogo = true
		result.LogoSHA256 = sha
		result.LogoWidth = width
		result.LogoHeight = height
		result.UpdatedAt = updatedAt
	}
	return result, nil
}

// GetPrintIdentity returns the doctor's matrícula configuration and clinic logo metadata.
func (s *Store) GetPrintIdentity(ctx context.Context, userID string) (PrintIdentity, error) {
	var result PrintIdentity
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		var err error
		result, err = readPrintIdentityTx(ctx, tx, userID)
		return err
	})
	return result, err
}

// UpdateProfessionalLicense updates or clears the doctor's optional professional license used in PDFs.
func (s *Store) UpdateProfessionalLicense(ctx context.Context, userID string, request map[string]any) (PrintIdentity, error) {
	var result PrintIdentity
	if err := api.Only(request, "professionalLicense"); err != nil {
		return result, err
	}
	opt, err := api.OptionalText(request, "professionalLicense")
	if err != nil {
		return result, err
	}
	raw := ""
	if opt != nil {
		raw = *opt
	}
	license, err := normalizeProfessionalLicense(raw)
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		workspaceID, err := ensureWorkspaceRow(ctx, tx, userID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE doctor_workspaces
			SET professional_license=$2, license_configured=true,
			    updated_at=greatest(clock_timestamp(), updated_at + interval '1 microsecond')
			WHERE id=$1`, workspaceID, license); err != nil {
			return err
		}
		result, err = readPrintIdentityTx(ctx, tx, userID)
		return err
	})
	return result, err
}

// UploadClinicLogo converts the uploaded image through the shared Bycode compressor and saves it.
func (s *Store) UploadClinicLogo(ctx context.Context, userID string, raw []byte) (PrintIdentity, error) {
	var result PrintIdentity
	if s.logoConverter == nil {
		return result, api.Failure(503, "IMAGE_CONVERTER_UNAVAILABLE", "El optimizador de imágenes no está disponible en este momento.")
	}
	converted, err := s.logoConverter.ConvertClinicLogo(ctx, raw)
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		workspaceID, err := ensureWorkspaceRow(ctx, tx, userID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO doctor_clinic_logos (workspace_id, image_webp, image_png, image_width, image_height, sha256, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp(), clock_timestamp())
			ON CONFLICT (workspace_id) DO UPDATE
			SET image_webp = EXCLUDED.image_webp,
			    image_png = EXCLUDED.image_png,
			    image_width = EXCLUDED.image_width,
			    image_height = EXCLUDED.image_height,
			    sha256 = EXCLUDED.sha256,
			    updated_at = greatest(clock_timestamp(), doctor_clinic_logos.updated_at + interval '1 microsecond')`,
			workspaceID, converted.WebP, converted.PNG, converted.Width, converted.Height, converted.SHA256); err != nil {
			return err
		}
		result, err = readPrintIdentityTx(ctx, tx, userID)
		return err
	})
	return result, err
}

// GetClinicLogoWebP returns the compressed WebP clinic logo bytes and SHA-256 ETag.
func (s *Store) GetClinicLogoWebP(ctx context.Context, userID string) ([]byte, string, error) {
	var webpBytes []byte
	var digest string
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT l.image_webp, l.sha256
			FROM doctor_clinic_logos l
			JOIN doctor_workspaces w ON w.id = l.workspace_id
			WHERE w.user_id = $1`, userID).Scan(&webpBytes, &digest)
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Failure(404, "CLINIC_LOGO_NOT_FOUND", "No hay un logo de consultorio cargado.")
		}
		return err
	})
	return webpBytes, digest, err
}

// DeleteClinicLogo removes the doctor's clinic logo.
func (s *Store) DeleteClinicLogo(ctx context.Context, userID string) error {
	return s.transaction(ctx, false, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM doctor_clinic_logos l
			USING doctor_workspaces w
			WHERE l.workspace_id = w.id AND w.user_id = $1`, userID)
		return err
	})
}

// ResolvePrintConfig returns the authoritative license (empty string when omitted/cleared)
// and active clinic logo asset for PDF rendering inside an existing clinical transaction.
func (s *Store) ResolvePrintConfig(ctx context.Context, tx pgx.Tx, userID, fallbackHeaderLicense string) (string, *LogoAsset, error) {
	var license string
	var configured bool
	var logoPNG []byte
	var width, height *int
	var sha *string
	err := tx.QueryRow(ctx, `SELECT w.professional_license, w.license_configured,
		l.image_png, l.image_width, l.image_height, l.sha256
		FROM doctor_workspaces w
		LEFT JOIN doctor_clinic_logos l ON l.workspace_id = w.id
		WHERE w.user_id = $1`, userID).Scan(&license, &configured, &logoPNG, &width, &height, &sha)
	if errors.Is(err, pgx.ErrNoRows) {
		return strings.TrimSpace(fallbackHeaderLicense), nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	resolvedLicense := strings.TrimSpace(fallbackHeaderLicense)
	if configured {
		resolvedLicense = strings.TrimSpace(license)
	}
	var logo *LogoAsset
	if len(logoPNG) > 0 && width != nil && height != nil && sha != nil {
		logo = &LogoAsset{
			PNG:    logoPNG,
			Width:  *width,
			Height: *height,
			SHA256: *sha,
		}
	}
	return resolvedLicense, logo, nil
}
