package dataexport

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

const maxBillingBytes = 8 << 20

func (s *Service) exportBilling(ctx context.Context, a *archive, owner string) (time.Time, error) {
	failure := api.Failure(http.StatusServiceUnavailable, "EXPORT_BILLING_UNAVAILABLE", "Los datos de cuenta y facturación no están disponibles. Volvé a intentar.")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.billingURL+"/internal/doctorchart/owners/"+url.PathEscape(owner)+"/export", nil)
	if err != nil {
		return time.Time{}, failure
	}
	req.Header.Set("X-Internal-Secret", s.billingSecret)
	response, err := s.client.Do(req)
	if err != nil {
		return time.Time{}, failure
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusRequestEntityTooLarge {
		return time.Time{}, exportTooLarge()
	}
	if response.StatusCode != http.StatusOK {
		return time.Time{}, failure
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBillingBytes+1))
	if err != nil || len(data) > maxBillingBytes {
		return time.Time{}, failure
	}

	var head struct {
		Owner      string    `json:"ownerId"`
		Module     string    `json:"module"`
		CapturedAt time.Time `json:"capturedAt"`
		Avatar     *struct {
			Type   string `json:"contentType"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
			Data   []byte `json:"data"`
		} `json:"avatar"`
	}
	if err := json.Unmarshal(data, &head); err != nil || head.Owner != owner || head.Module != "DOCTORCHART" || head.CapturedAt.IsZero() {
		return time.Time{}, failure
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return time.Time{}, failure
	}
	for field := range payload {
		switch field {
		case "ownerId", "module", "capturedAt", "profileCapturedAt", "profile", "coverage", "orderItems",
			"renewals", "renewalCycles", "paymentAttempts", "grants", "transfers":
		default:
			delete(payload, field)
		}
	}
	if avatar := head.Avatar; avatar != nil {
		if avatar.Type != "image/webp" || avatar.Size < 1 || int64(len(avatar.Data)) != avatar.Size || hex.EncodeToString(calculateSHA(avatar.Data)) != avatar.SHA256 {
			return time.Time{}, attachmentUnavailable()
		}
		const path = "adjuntos/perfil.webp"
		if err := a.add(path, func(w io.Writer) error { _, err := w.Write(avatar.Data); return err }); err != nil {
			return time.Time{}, err
		}
		summary := struct {
			Path   string `json:"path"`
			Bytes  int64  `json:"bytes"`
			SHA256 string `json:"sha256"`
		}{path, avatar.Size, avatar.SHA256}
		payload["avatar"], err = json.Marshal(summary)
		if err != nil {
			return time.Time{}, err
		}
	}
	if err := a.addJSON("datos/facturacion.json", payload); err != nil {
		return time.Time{}, err
	}
	if err := a.add("lectura/facturacion.html", func(w io.Writer) error {
		if head.Avatar == nil {
			return writeObjectPage(w, "Cuenta y facturación", payload)
		}
		if _, err := io.WriteString(w, pageStart+`<p><a href="../INDICE.html">Volver al índice</a></p><h1>Cuenta y facturación</h1><p><a href="../adjuntos/perfil.webp">Abrir foto de perfil</a></p>`); err != nil {
			return err
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if err := writePrettyJSON(w, raw); err != nil {
			return err
		}
		_, err = io.WriteString(w, "</html>")
		return err
	}); err != nil {
		return time.Time{}, err
	}
	return head.CapturedAt, nil
}
