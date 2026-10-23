package clinical

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type upcomingFollowUp struct {
	EncounterID int64  `json:"encounterId"`
	PatientID   int64  `json:"patientId"`
	PatientName string `json:"patientFullName"`
	Date        string `json:"followUpDate"`
}

// A newer encounter replaces the previous follow-up, including when the doctor
// clears the date. Filtering dates before selecting the latest would revive old
// instructions or show several reminders for the same patient.
func dashboardFollowUps(ctx context.Context, tx pgx.Tx, owner, from, until string) ([]upcomingFollowUp, error) {
	rows, err := tx.Query(ctx, `SELECT e.id,p.id,p.full_name,e.follow_up_date::text
		FROM patients p
		JOIN LATERAL (
			SELECT id,follow_up_date FROM clinical_encounters WHERE patient_id=p.id
			ORDER BY encounter_date DESC,id DESC LIMIT 1
		) e ON true
		WHERE p.user_id=$1 AND e.follow_up_date >= $2::date AND e.follow_up_date < $3::date
		ORDER BY e.follow_up_date,lower(p.full_name),p.id`, owner, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []upcomingFollowUp{}
	for rows.Next() {
		var item upcomingFollowUp
		if err := rows.Scan(&item.EncounterID, &item.PatientID, &item.PatientName, &item.Date); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
