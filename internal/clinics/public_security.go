package clinics

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var opaqueID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var networkID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func newOpaqueID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure random unavailable")
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
func publicMissing() error {
	return api.Failure(404, "PUBLIC_PROFILE_NOT_FOUND", "El profesional o consultorio no está disponible para reservas online.")
}
func publicDenied() error {
	return api.Failure(403, "ONLINE_BOOKING_DISABLED", "Las reservas online no están habilitadas en este momento.")
}
func publicHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
func (s *Service) publicLimit(r *http.Request, kind string, max int) error {
	subject := r.Header.Get("X-Public-Client")
	if !networkID.MatchString(subject) {
		return api.Failure(403, "INVALID_PUBLIC_CONTEXT", "La solicitud requiere el acceso público autorizado.")
	}
	var used int
	minute := time.Now().Unix() / 60
	if err := s.pool.QueryRow(r.Context(), "INSERT INTO public_booking_rate_windows(subject,kind,minute,used) VALUES($1,$2,$3,1) ON CONFLICT(subject,kind,minute) DO UPDATE SET used=public_booking_rate_windows.used+1 RETURNING used", subject, kind, minute).Scan(&used); err != nil {
		return err
	}
	// Retain two hours of anonymized counters; avoid indefinitely growing rate state.
	if used == 1 {
		if _, err := s.pool.Exec(r.Context(), "DELETE FROM public_booking_rate_windows WHERE minute<$1", minute-120); err != nil {
			return err
		}
	}
	if used > max {
		return api.Failure(429, "PUBLIC_RATE_LIMITED", "Demasiadas solicitudes. Esperá un minuto y reintentá.")
	}
	return nil
}

type proofClaims struct {
	Network string `json:"network"`
	Nonce   string `json:"nonce"`
	Expires int64  `json:"expires"`
}

func (s *Service) proofMAC(payload string) []byte {
	m := hmac.New(sha256.New, []byte(s.publicSecret))
	m.Write([]byte("doctorchart-public-pow-v1\n" + payload))
	return m.Sum(nil)
}
func (s *Service) challenge(w http.ResponseWriter, r *http.Request) error {
	var body map[string]any
	if err := decodePublic(w, r, &body); err != nil || len(body) != 0 {
		return api.Invalid("Desafío inválido.")
	}
	claims := proofClaims{r.Header.Get("X-Public-Client"), newOpaqueID(), time.Now().Add(5 * time.Minute).Unix()}
	raw, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	token := payload + "." + hex.EncodeToString(s.proofMAC(payload))
	api.WriteJSON(w, 200, map[string]any{"token": token, "difficulty": 18})
	return nil
}
func (s *Service) verifyProof(token, solution, network string) (string, error) {
	if len(token) > 1024 || len(solution) > 12 {
		return "", publicDenied()
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", publicDenied()
	}
	signature, err := hex.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, s.proofMAC(parts[0])) {
		return "", publicDenied()
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	var claims proofClaims
	if err != nil || json.Unmarshal(raw, &claims) != nil || claims.Network != network || claims.Expires < time.Now().Unix() || claims.Expires > time.Now().Add(5*time.Minute).Unix() {
		return "", publicDenied()
	}
	n, err := strconv.ParseUint(solution, 10, 40)
	if err != nil || strconv.FormatUint(n, 10) != solution {
		return "", publicDenied()
	}
	sum := sha256.Sum256([]byte(token + "." + solution))
	if sum[0] != 0 || sum[1] != 0 || sum[2]&192 != 0 {
		return "", api.Failure(403, "INVALID_CHALLENGE", "La verificación automática venció o no es válida. Reintentá.")
	}
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:]), nil
}
func decodePublic(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return api.Invalid("Revisá los datos enviados.")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return api.Invalid("Una sola solicitud por envío.")
	}
	return nil
}
func (s *Service) publicWrap(kind string, max int, fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		publicHeaders(w)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		err := s.publicLimit(r, kind, max)
		if err == nil {
			err = fn(w, r)
		}
		if err != nil {
			if failure, ok := err.(*api.Error); ok && failure.Status == 429 {
				w.Header().Set("Retry-After", "60")
			}
			api.WriteError(w, err)
		}
	}
}
