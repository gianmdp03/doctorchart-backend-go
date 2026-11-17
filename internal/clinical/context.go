package clinical

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/db"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func (s *Service) beginClinicalTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := s.pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	if err := db.BindClinicalScope(ctx, tx, api.User(ctx).UserID, api.SpecialtyContextID(ctx)); err != nil {
		_ = rollbackClinicalTx(ctx, tx)
		return nil, err
	}
	return tx, nil
}

func rollbackClinicalTx(ctx context.Context, tx pgx.Tx) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return tx.Rollback(cleanup)
}

// Every clinical HTTP entry point binds an explicit immutable request scope.
// Query context also works for multipart/files/PDF without new CORS headers.
func (s *Service) handle(mux *http.ServeMux, pattern string, fn http.HandlerFunc) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		var audit *accessWriter
		if operation := accessOperations[pattern]; operation != "" {
			audit = s.accessBoundary(w, r, operation)
			w = audit
			r = r.WithContext(context.WithValue(r.Context(), accessKey{}, audit))
		}
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
		if audit != nil {
			audit.contextID = &id
		}
		if !setup.Completed || len(setup.Definition) == 0 {
			api.WriteError(w, api.Failure(409, "ONBOARDING_REQUIRED", "Terminá la configuración de esta especialidad antes de atender."))
			return
		}
		fn(w, r.WithContext(ctx))
	})
}
