package workspace

import (
	"net/http"
	"strconv"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type handler func(http.ResponseWriter, *http.Request) (any, int, error)

func register(mux *http.ServeMux, route string, fn handler) {
	mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
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
}
