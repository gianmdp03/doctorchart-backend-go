package workspace

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type handler func(http.ResponseWriter, *http.Request) (any, int, error)

func register(mux *http.ServeMux, route string, fn handler) {
	mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
		if err := bindContextQuery(r); err != nil {
			api.WriteError(w, err)
			return
		}
		value, status, err := fn(w, r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		if value == nil {
			w.WriteHeader(status)
			return
		}
		api.WriteJSON(w, status, value)
	})
}
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, api.Invalid("La solicitud contiene datos inválidos.")
	}
	return id, nil
}

func (s *Store) Routes(mux *http.ServeMux) {
	s.reviewRoutes(mux)
	register(mux, "GET /api/doctorchart/workspace/contexts", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		values, err := s.Contexts(r.Context(), api.User(r.Context()).UserID)
		return values, 200, err
	})
	register(mux, "POST /api/doctorchart/workspace/contexts", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		body, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		value, err := s.AddContext(r.Context(), api.User(r.Context()).UserID, body)
		return value, 201, err
	})
	register(mux, "GET /api/doctorchart/specialties", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		values, err := s.Specialties(r.Context(), false)
		summaries := []Summary{}
		for _, value := range values {
			summaries = append(summaries, value.summary())
		}
		return summaries, 200, err
	})
	register(mux, "GET /api/doctorchart/admin/specialties", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		value, err := s.Specialties(r.Context(), true)
		return value, 200, err
	})
	register(mux, "POST /api/doctorchart/admin/specialties", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		value, err := s.CreateSpecialty(r.Context(), request)
		if err == nil {
			w.Header().Set("Location", "/api/doctorchart/admin/specialties/"+strconv.FormatInt(value.ID, 10))
		}
		return value, 201, err
	})
	register(mux, "PATCH /api/doctorchart/admin/specialties/{id}", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, 0, err
		}
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		value, err := s.UpdateSpecialty(r.Context(), id, request)
		return value, 200, err
	})
	for route, published := range map[string]bool{"GET /api/doctorchart/specialties/{id}/wizard": true, "GET /api/doctorchart/admin/specialties/{id}/wizard": false} {
		register(mux, route, func(w http.ResponseWriter, r *http.Request) (any, int, error) {
			id, err := pathID(r)
			if err != nil {
				return nil, 0, err
			}
			value, err := s.Wizard(r.Context(), id, published)
			return value, 200, err
		})
	}
	for route, publish := range map[string]bool{"PUT /api/doctorchart/admin/specialties/{id}/wizard/draft": false, "POST /api/doctorchart/admin/specialties/{id}/wizard/publish": true} {
		register(mux, route, func(w http.ResponseWriter, r *http.Request) (any, int, error) {
			id, err := pathID(r)
			if err != nil {
				return nil, 0, err
			}
			request, err := api.Decode(w, r)
			if err != nil {
				return nil, 0, err
			}
			value, err := s.SaveWizard(r.Context(), id, request, publish)
			return value, 200, err
		})
	}
	for route, value := range map[string][]string{"GET /api/doctorchart/admin/specialties/experience-navigation": navigation, "GET /api/doctorchart/admin/specialties/experience-record-tabs": tabs, "GET /api/doctorchart/admin/specialties/experience-record-actions": actions} {
		register(mux, route, func(w http.ResponseWriter, r *http.Request) (any, int, error) { return value, 200, nil })
	}
	register(mux, "GET /api/doctorchart/admin/specialties/{id}/experience", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, 0, err
		}
		value, err := s.Experience(r.Context(), id)
		return value, 200, err
	})
	for route, key := range map[string]string{"PUT /api/doctorchart/admin/specialties/{id}/experience": "navigation", "PUT /api/doctorchart/admin/specialties/{id}/experience/patient-record-tabs": "patientRecordTabs", "PUT /api/doctorchart/admin/specialties/{id}/experience/patient-record-actions": "patientRecordActions"} {
		register(mux, route, func(w http.ResponseWriter, r *http.Request) (any, int, error) {
			id, err := pathID(r)
			if err != nil {
				return nil, 0, err
			}
			request, err := api.Decode(w, r)
			if err != nil {
				return nil, 0, err
			}
			value, err := s.UpdateExperience(r.Context(), id, key, request)
			return value, 200, err
		})
	}
	register(mux, "GET /api/doctorchart/workspace", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		value, err := s.Setup(r.Context(), api.User(r.Context()).UserID)
		if value == nil {
			return nil, 200, err
		}
		return value, 200, err
	})
	register(mux, "GET /api/doctorchart/doctor/profile", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		value, err := s.Profile(r.Context(), api.User(r.Context()).UserID)
		status := 200
		if value == nil {
			status = 404
		}
		return value, status, err
	})
	register(mux, "PUT /api/doctorchart/workspace/specialty", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		value, err := s.Select(r.Context(), api.User(r.Context()).UserID, request)
		return value, 200, err
	})
	for route, operation := range map[string]string{"PUT /api/doctorchart/workspace/wizard/draft": "draft", "POST /api/doctorchart/workspace/wizard/discard-draft": "discard", "POST /api/doctorchart/workspace/finish-onboarding": "finish", "POST /api/doctorchart/workspace/reset-to-defaults": "reset"} {
		register(mux, route, func(w http.ResponseWriter, r *http.Request) (any, int, error) {
			request, err := api.Decode(w, r)
			if err != nil {
				return nil, 0, err
			}
			value, err := s.Edit(r.Context(), api.User(r.Context()).UserID, operation, request)
			return value, 200, err
		})
	}
	register(mux, "GET /api/doctorchart/workspace/encounter-templates", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		user := api.User(r.Context())
		if user.HasRole("STAFF") {
			return nil, 0, api.Failure(403, "FORBIDDEN", "Solo el profesional titular puede administrar las plantillas de historia clínica.")
		}
		value, err := s.ListEncounterTemplates(r.Context(), user.UserID)
		return value, 200, err
	})
	register(mux, "POST /api/doctorchart/workspace/encounter-templates", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		user := api.User(r.Context())
		if user.HasRole("STAFF") {
			return nil, 0, api.Failure(403, "FORBIDDEN", "Solo el profesional titular puede administrar las plantillas de historia clínica.")
		}
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		value, err := s.CreateEncounterTemplate(r.Context(), user.UserID, request)
		return value, 201, err
	})
	register(mux, "PUT /api/doctorchart/workspace/encounter-templates/{id}", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		user := api.User(r.Context())
		if user.HasRole("STAFF") {
			return nil, 0, api.Failure(403, "FORBIDDEN", "Solo el profesional titular puede administrar las plantillas de historia clínica.")
		}
		id, err := pathID(r)
		if err != nil {
			return nil, 0, err
		}
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		value, err := s.UpdateEncounterTemplate(r.Context(), user.UserID, id, request)
		return value, 200, err
	})
	register(mux, "POST /api/doctorchart/workspace/encounter-templates/{id}/archive", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		user := api.User(r.Context())
		if user.HasRole("STAFF") {
			return nil, 0, api.Failure(403, "FORBIDDEN", "Solo el profesional titular puede administrar las plantillas de historia clínica.")
		}
		id, err := pathID(r)
		if err != nil {
			return nil, 0, err
		}
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		value, err := s.ArchiveEncounterTemplate(r.Context(), user.UserID, id, request)
		return value, 200, err
	})
	register(mux, "POST /api/doctorchart/workspace/encounter-templates/{id}/restore", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		user := api.User(r.Context())
		if user.HasRole("STAFF") {
			return nil, 0, api.Failure(403, "FORBIDDEN", "Solo el profesional titular puede administrar las plantillas de historia clínica.")
		}
		id, err := pathID(r)
		if err != nil {
			return nil, 0, err
		}
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		value, err := s.RestoreEncounterTemplate(r.Context(), user.UserID, id, request)
		return value, 200, err
	})
	register(mux, "POST /api/doctorchart/workspace/encounter-templates/import-defaults", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		user := api.User(r.Context())
		if user.HasRole("STAFF") {
			return nil, 0, api.Failure(403, "FORBIDDEN", "Solo el profesional titular puede administrar las plantillas de historia clínica.")
		}
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		value, err := s.ImportDefaultEncounterTemplates(r.Context(), user.UserID, request)
		return value, 200, err
	})
	register(mux, "GET /api/doctorchart/workspace/preferences", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		value, err := s.Preferences(r.Context(), api.User(r.Context()).UserID)
		return value, 200, err
	})
	for route, notes := range map[string]bool{"PUT /api/doctorchart/workspace/preferences": false, "PUT /api/doctorchart/workspace/quick-notes": true} {
		register(mux, route, func(w http.ResponseWriter, r *http.Request) (any, int, error) {
			request, err := api.Decode(w, r)
			if err != nil {
				return nil, 0, err
			}
			value, err := s.UpdatePreferences(r.Context(), api.User(r.Context()).UserID, request, notes)
			return value, 200, err
		})
	}
	register(mux, "GET /api/doctorchart/workspace/ai-access", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		err := s.RequireFeature(r.Context(), api.User(r.Context()).UserID, "AI_ASSISTANT")
		return nil, 204, err
	})
	register(mux, "GET /api/doctorchart/workspace/signatures", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		items, err := s.ListSignatures(r.Context(), api.User(r.Context()).UserID)
		return map[string]any{"items": items}, 200, err
	})
	register(mux, "POST /api/doctorchart/workspace/signatures", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		item, err := s.CreateSignature(r.Context(), api.User(r.Context()).UserID, request)
		return item, 201, err
	})
	register(mux, "PATCH /api/doctorchart/workspace/signatures/{id}", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, 0, err
		}
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		item, err := s.UpdateSignature(r.Context(), api.User(r.Context()).UserID, id, request)
		return item, 200, err
	})
	register(mux, "DELETE /api/doctorchart/workspace/signatures/{id}", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, 0, err
		}
		err = s.DeleteSignature(r.Context(), api.User(r.Context()).UserID, id)
		return nil, 204, err
	})
	mux.HandleFunc("GET /api/doctorchart/workspace/signatures/{id}/image", func(w http.ResponseWriter, r *http.Request) {
		if err := bindContextQuery(r); err != nil {
			api.WriteError(w, err)
			return
		}
		id, err := pathID(r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		pngBytes, digest, err := s.SignatureImage(r.Context(), api.User(r.Context()).UserID, id)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		etag := `"` + digest + `"`
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
		http.ServeContent(w, r, "signature.png", time.Time{}, bytes.NewReader(pngBytes))
	})
	register(mux, "GET /api/doctorchart/workspace/print-identity", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		identity, err := s.GetPrintIdentity(r.Context(), api.User(r.Context()).UserID)
		return identity, 200, err
	})
	register(mux, "PUT /api/doctorchart/workspace/print-identity", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		user := api.User(r.Context())
		if user.HasRole("STAFF") {
			return nil, 0, api.Failure(403, "FORBIDDEN", "Solo el profesional titular puede modificar la matrícula de impresión.")
		}
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		identity, err := s.UpdateProfessionalLicense(r.Context(), user.UserID, request)
		return identity, 200, err
	})
	register(mux, "POST /api/doctorchart/workspace/logo", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		user := api.User(r.Context())
		if user.HasRole("STAFF") {
			return nil, 0, api.Failure(403, "FORBIDDEN", "Solo el profesional titular puede modificar el logo del consultorio.")
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxLogoUploadBytes+1<<20)
		if err := r.ParseMultipartForm(maxLogoUploadBytes); err != nil {
			var tooBig *http.MaxBytesError
			if err == http.ErrNotMultipart {
				return nil, 0, api.Invalid("Enviá la imagen del logo como multipart/form-data.")
			}
			if _, ok := err.(*http.MaxBytesError); ok || tooBig != nil {
				return nil, 0, api.Failure(413, "LOGO_FILE_TOO_LARGE", "El logo supera el tamaño máximo permitido de 10 MB.")
			}
			return nil, 0, api.Invalid("No se pudo leer el archivo del logo.")
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			return nil, 0, api.Failure(400, "INVALID_LOGO_IMAGE", "Seleccioná un archivo de imagen en el campo file.")
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, maxLogoUploadBytes+1))
		if err != nil {
			return nil, 0, api.Invalid("No se pudo leer la imagen del logo.")
		}
		if len(raw) > maxLogoUploadBytes {
			return nil, 0, api.Failure(413, "LOGO_FILE_TOO_LARGE", "El logo supera el tamaño máximo permitido de 10 MB.")
		}
		identity, err := s.UploadClinicLogo(r.Context(), user.UserID, raw)
		return identity, 200, err
	})
	register(mux, "DELETE /api/doctorchart/workspace/logo", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		user := api.User(r.Context())
		if user.HasRole("STAFF") {
			return nil, 0, api.Failure(403, "FORBIDDEN", "Solo el profesional titular puede eliminar el logo del consultorio.")
		}
		err := s.DeleteClinicLogo(r.Context(), user.UserID)
		return nil, 204, err
	})
	mux.HandleFunc("GET /api/doctorchart/workspace/logo", func(w http.ResponseWriter, r *http.Request) {
		if err := bindContextQuery(r); err != nil {
			api.WriteError(w, err)
			return
		}
		webpBytes, digest, err := s.GetClinicLogoWebP(r.Context(), api.User(r.Context()).UserID)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		etag := `"` + digest + `"`
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "clinic-logo.webp", time.Time{}, bytes.NewReader(webpBytes))
	})
}
