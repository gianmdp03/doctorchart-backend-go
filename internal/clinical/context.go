package clinical

import (
	"net/http"
	"strconv"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

// Every clinical HTTP entry point binds an explicit immutable request scope.
// Query context also works for multipart/files/PDF without new CORS headers.
func (s *Service) handle(mux *http.ServeMux, pattern string, fn http.HandlerFunc) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()["specialtyContextId"]
		if len(values) != 1 || values[0] == "" {
			api.WriteError(w, api.Failure(400, "SPECIALTY_CONTEXT_REQUIRED", "Indicá el contexto clínico de la solicitud."))
			return
		}
		id, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || id <= 0 {
			api.WriteError(w, api.Invalid("El contexto clínico no es válido."))
			return
		}
		ctx := api.WithSpecialtyContext(r.Context(), id)
		// Upload admission/size checks must still precede database acquisition.
		// studyOwner checks the scoped patient again before conversion and commit.
		if pattern == "POST /api/doctorchart/patients/{patientId}/studies" {
			fn(w, r.WithContext(ctx))
			return
		}
		setup, err := s.workspaces.Setup(ctx, api.User(ctx).UserID)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		if setup == nil {
			api.WriteError(w, api.Failure(404, "SPECIALTY_CONTEXT_NOT_FOUND", "El contexto clínico no está disponible."))
			return
		}
		if !setup.Completed || len(setup.Definition) == 0 {
			api.WriteError(w, api.Failure(409, "ONBOARDING_REQUIRED", "Terminá la configuración de esta especialidad antes de atender."))
			return
		}
		fn(w, r.WithContext(ctx))
	})
}
