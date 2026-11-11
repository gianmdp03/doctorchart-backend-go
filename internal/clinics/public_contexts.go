package clinics

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type PublicContext struct {
	ID      string         `json:"id"`
	Code    string         `json:"code"`
	Name    string         `json:"name"`
	Clinics []PublicClinic `json:"clinics"`
}

const publicOfferExists = `EXISTS(SELECT 1 FROM schedule_blocks b LEFT JOIN schedule_exceptions ex ON ex.id=b.exception_id WHERE b.specialty_context_id=cctx.id AND b.clinic_id=c.id AND (b.weekday IS NOT NULL OR ex.through_date>=CURRENT_DATE))`

func publicContextClinic(d PublicDoctor, context, clinic string) bool {
	for _, s := range d.Specialties {
		if s.ID == context {
			for _, c := range s.Clinics {
				if c.ID == clinic {
					return true
				}
			}
		}
	}
	return false
}
func choosePublicContext(d PublicDoctor, id string) (string, error) {
	if id == "" {
		if len(d.Specialties) == 0 {
			return "", api.Failure(409, "BOOKING_SLOT_UNAVAILABLE", "Ese horario ya no está disponible.")
		}
		if len(d.Specialties) == 1 {
			return d.Specialties[0].ID, nil
		}
		return "", api.Failure(400, "SPECIALTY_CONTEXT_REQUIRED", "Elegí la especialidad del turno.")
	}
	if _, err := decimal(id, true); err != nil {
		return "", err
	}
	for _, s := range d.Specialties {
		if s.ID == id {
			return id, nil
		}
	}
	return "", publicMissing()
}
func readPublicContexts(ctx context.Context, q scheduleReader, workspace int64) ([]PublicContext, error) {
	rows, err := q.Query(ctx, `SELECT cctx.id::text,s.code,s.display_name,cp.public_id::text,c.name,c.country,c.province,c.city,cp.address,c.timezone
 FROM publication_specialty_contexts pc JOIN doctor_specialty_contexts cctx ON cctx.id=pc.specialty_context_id AND cctx.workspace_id=pc.workspace_id
 JOIN specialties s ON s.id=cctx.specialty_id JOIN clinic_publications cp ON cp.workspace_id=pc.workspace_id JOIN clinics c ON c.id=cp.clinic_id
 WHERE pc.workspace_id=$1 AND cctx.onboarding_completed AND cctx.definition IS NOT NULL AND s.active AND cp.enabled AND c.status='ACTIVE' AND `+publicOfferExists+` ORDER BY s.display_name,cctx.id,c.name,c.id`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PublicContext{}
	index := map[string]int{}
	for rows.Next() {
		var s PublicContext
		var c PublicClinic
		if err = rows.Scan(&s.ID, &s.Code, &s.Name, &c.ID, &c.Name, &c.Country, &c.Province, &c.City, &c.Address, &c.Timezone); err != nil {
			return nil, err
		}
		i, ok := index[s.ID]
		if !ok {
			i = len(items)
			index[s.ID] = i
			s.Clinics = []PublicClinic{}
			items = append(items, s)
		}
		items[i].Clinics = append(items[i].Clinics, c)
	}
	return items, rows.Err()
}
func publicationContexts(ctx context.Context, tx pgx.Tx, workspace int64, ids []string) error {
	if ids == nil {
		rows, err := tx.Query(ctx, "SELECT specialty_context_id::text FROM publication_specialty_contexts WHERE workspace_id=$1 ORDER BY specialty_context_id", workspace)
		if err != nil {
			return err
		}
		ids = []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			var count int
			var only string
			if err = tx.QueryRow(ctx, "SELECT count(*),COALESCE(min(id)::text,'') FROM doctor_specialty_contexts WHERE workspace_id=$1", workspace).Scan(&count, &only); err != nil {
				return err
			}
			if count == 1 {
				ids = append(ids, only)
			} else {
				return api.Failure(400, "SPECIALTY_CONTEXT_REQUIRED", "Elegí las especialidades de la publicación.")
			}
		}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if _, err := decimal(id, true); err != nil {
			return err
		}
		if seen[id] {
			return api.Invalid("La especialidad está repetida.")
		}
		seen[id] = true
		var ready bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM doctor_specialty_contexts WHERE id=$1 AND workspace_id=$2 AND onboarding_completed AND definition IS NOT NULL)", id, workspace).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return api.Failure(404, "SPECIALTY_CONTEXT_NOT_FOUND", "La especialidad no está lista para publicarse.")
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM publication_specialty_contexts WHERE workspace_id=$1", workspace); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, "INSERT INTO publication_specialty_contexts(workspace_id,specialty_context_id) VALUES($1,$2)", workspace, id); err != nil {
			return err
		}
	}
	return nil
}
