package dataexport

import (
	"fmt"
	"strings"
)

type section struct {
	name   string
	table  string
	joins  string
	where  string
	order  string
	fields string
}

var workspaceSections = []section{
	{"espacio", "doctor_workspaces", "", `x.user_id = $1`, "x.id", "id created_at patient_columns_config quick_notes updated_at user_id version professional_license license_configured"},
	{"especialidades", "doctor_specialty_contexts", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.id", "id workspace_id specialty_id definition draft_definition source_wizard_version draft_source_wizard_version version current_step_key onboarding_completed created_at updated_at teleconsultation_enabled"},
	{"preferencias", "specialty_context_preferences", `JOIN doctor_specialty_contexts c ON c.id = x.specialty_context_id JOIN doctor_workspaces w ON w.id = c.workspace_id`, `w.user_id = $1`, "x.specialty_context_id", "specialty_context_id version patient_columns_config quick_notes"},
	{"graficos", "doctor_chart_configs", "", `x.user_id = $1`, "x.id", "id chart_code field_mappings is_active user_id"},
	{"preferencias-calculadoras", "doctor_calculator_preferences", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.id", "id created_at updated_at specialty_id workspace_id"},
	{"selecciones-calculadoras", "doctor_calculator_selections", `JOIN doctor_calculator_preferences p ON p.id = x.preference_id JOIN doctor_workspaces w ON w.id = p.workspace_id`, `w.user_id = $1`, "x.preference_id, x.calculator_id", "preference_id calculator_id"},
	{"referencias-especialidades", "specialties", "", `EXISTS (SELECT 1 FROM doctor_specialty_contexts c JOIN doctor_workspaces w ON w.id = c.workspace_id WHERE w.user_id = $1 AND c.specialty_id = x.id)`, "x.id", "id active code created_at description display_name navigation patient_record_actions patient_record_tabs updated_at version"},
	{"referencias-graficos", "chart_definitions", "", `EXISTS (SELECT 1 FROM doctor_chart_configs p WHERE p.user_id = $1 AND p.chart_code = x.chart_code)`, "x.id", "id chart_code chart_type description is_system required_slots title"},
	{"referencias-calculadoras", "clinical_calculation_definitions", "", `EXISTS (SELECT 1 FROM doctor_calculator_preferences p JOIN doctor_workspaces w ON w.id=p.workspace_id JOIN doctor_calculator_selections s ON s.preference_id=p.id WHERE w.user_id=$1 AND s.calculator_id=x.id) OR EXISTS (SELECT 1 FROM specialty_calculator_defaults d JOIN doctor_specialty_contexts c ON c.specialty_id=d.specialty_id JOIN doctor_workspaces w ON w.id=c.workspace_id WHERE w.user_id=$1 AND d.calculator_id=x.id)`, "x.id", "id category category_label code description display_order evidence_reference is_system name required_parameters result_unit short_name summary_formula"},
	{"referencias-calculadoras-especialidad", "specialty_calculator_defaults", "", `EXISTS (SELECT 1 FROM doctor_specialty_contexts c JOIN doctor_workspaces w ON w.id = c.workspace_id WHERE w.user_id = $1 AND c.specialty_id = x.specialty_id)`, "x.id", "id calculator_id specialty_id"},
	{"referencias-wizards", "wizard_defaults", "", `EXISTS (SELECT 1 FROM doctor_specialty_contexts c JOIN doctor_workspaces w ON w.id = c.workspace_id WHERE w.user_id = $1 AND c.specialty_id = x.specialty_id)`, "x.id", "published_approval_id id published_definition published_version specialty_id"},
	{"plantillas-historia-clinica", "doctor_encounter_templates", "JOIN doctor_workspaces w ON w.id = x.workspace_id", "w.user_id = $1", "x.specialty_context_id, x.display_order, x.id", "id workspace_id specialty_context_id name form_definition source_default_code display_order version archived_at created_at updated_at"},
	{"referencias-plantillas-historia", "specialty_encounter_template_defaults", "", `EXISTS (SELECT 1 FROM doctor_specialty_contexts c JOIN doctor_workspaces w ON w.id = c.workspace_id WHERE w.user_id = $1 AND c.specialty_id = x.specialty_id)`, "x.specialty_id, x.display_order, x.id", "id specialty_id code name description form_definition display_order active created_at updated_at"},
	{"referencias-versiones-wizard", "wizard_approvals", "", `EXISTS (SELECT 1 FROM doctor_specialty_contexts c JOIN doctor_workspaces w ON w.id=c.workspace_id JOIN wizard_defaults d ON d.specialty_id=c.specialty_id WHERE w.user_id=$1 AND x.specialty_id=c.specialty_id AND (x.id=d.published_approval_id OR x.revision IN (c.source_wizard_version,c.draft_source_wizard_version)))`, "x.id", "id specialty_id revision content fingerprint kind occurred_at"},
	{"referencias-revocaciones-wizard", "wizard_approval_revocations", `JOIN wizard_approvals a ON a.id=x.approval_id`, `EXISTS (SELECT 1 FROM doctor_specialty_contexts c JOIN doctor_workspaces w ON w.id=c.workspace_id JOIN wizard_defaults d ON d.specialty_id=c.specialty_id WHERE w.user_id=$1 AND a.specialty_id=c.specialty_id AND (a.id=d.published_approval_id OR a.revision IN (c.source_wizard_version,c.draft_source_wizard_version)))`, "x.approval_id", "approval_id reason occurred_at"},
	{"consultorios", "clinics", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.id", "id workspace_id name country province city address timezone status version created_at updated_at modality"},
	{"horarios", "clinic_schedules", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.clinic_id", "clinic_id min_notice_minutes max_advance_days version workspace_id"},
	{"consultorios-especialidades", "clinic_specialty_contexts", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.clinic_id, x.specialty_context_id", "clinic_id workspace_id specialty_context_id modality"},
	{"bloques-agenda", "schedule_blocks", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.id", "id clinic_id workspace_id specialty_context_id weekday exception_id start_minute end_minute slot_minutes"},
	{"excepciones-agenda", "schedule_exceptions", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.id", "id clinic_id workspace_id from_date through_date label"},
	{"publicacion-agenda", "booking_publications", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.workspace_id", "workspace_id public_id name description published version questions"},
	{"publicaciones-consultorios", "clinic_publications", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.clinic_id", "clinic_id workspace_id public_id enabled address modality"},
	{"publicaciones-especialidades", "publication_specialty_contexts", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.workspace_id, x.specialty_context_id", "workspace_id specialty_context_id"},
	{"turnos", "bookings", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.id", "id workspace_id clinic_id patient_id start_at end_at status version contact_name contact_phone created_at updated_at attendee_name specialty_context_id specialty_snapshot clinic_snapshot provenance"},
	{"historial-turnos", "booking_events", `JOIN bookings b ON b.id = x.booking_id JOIN doctor_workspaces w ON w.id = b.workspace_id`, `w.user_id = $1`, "x.booking_id, x.version", "booking_id version kind actor_id occurred_at before_document after_document"},
	{"respuestas-reservas", "booking_public_details", `JOIN bookings b ON b.id = x.booking_id JOIN doctor_workspaces w ON w.id = b.workspace_id`, `w.user_id = $1`, "x.booking_id", "booking_id reference form_version form_snapshot answers document_country document_type document email doctor_name clinic_name address timezone"},
	{"delegaciones", "staff_access", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.id", "id workspace_id account_id email name phone permissions active version"},
	{"delegaciones-consultorios", "staff_access_clinics", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.access_id, x.clinic_id", "access_id workspace_id clinic_id"},
	{"equipo", "staff_accounts", "", `EXISTS (SELECT 1 FROM staff_access a JOIN doctor_workspaces w ON w.id=a.workspace_id WHERE w.user_id=$1 AND a.account_id=x.id)`, "x.id", "id email name"},
	{"historial-equipo", "staff_security_events", `JOIN staff_access a ON a.id=x.access_id JOIN doctor_workspaces w ON w.id=a.workspace_id`, `w.user_id = $1`, "x.id", "id account_id access_id kind occurred_at"},
	{"historial-notificaciones", "notification_outbox", `LEFT JOIN bookings b ON b.id=x.booking_id LEFT JOIN staff_access a ON a.id=x.access_id JOIN doctor_workspaces w ON w.id=COALESCE(b.workspace_id,a.workspace_id)`, `w.user_id = $1 AND x.recovery_id IS NULL AND x.portal_code_id IS NULL`, "x.id", "id kind booking_id booking_version access_id due_at state created_at sent_at"},
	{"firmas", "doctor_signatures", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.id", "id workspace_id name stamp_text image_width image_height sha256 is_favorite created_at updated_at"},
	{"logo-consultorio", "doctor_clinic_logos", `JOIN doctor_workspaces w ON w.id = x.workspace_id`, `w.user_id = $1`, "x.workspace_id", "workspace_id image_width image_height sha256 created_at updated_at"},
}

var contextSections = []section{
	{"fichas", "patients", "", `x.user_id = $1 AND x.specialty_context_id = $2`, "x.id", "id annulled_at address allergies birth_date blood_type chronic_conditions email family_history form_definition_snapshot form_values formula_context formula_results full_name gender guardian_info insurance_info national_id personal_history phone updated_at user_id version workspace_version workspace_id specialty_context_id"},
	{"consultas", "clinical_encounters", `JOIN patients p ON p.id = x.patient_id`, `p.user_id = $1 AND p.specialty_context_id = $2`, "x.patient_id, x.id", "id annulled_at chief_complaint clinical_impression consultation_type diagnostic_orders disease encounter_date encounter_template_id encounter_template_name follow_up_date follow_up_instructions form_definition_snapshot form_values formula_context formula_results head_circumference_cm height_cm history_of_present_illness physical_examination prescriptions primary_diagnosis_code primary_diagnosis_description secondary_diagnoses treatment_plan updated_at version vital_signs weight_kg workspace_version patient_id specialty_id"},
	{"calculos", "patient_calculation_logs", `JOIN patients p ON p.id = x.patient_id`, `p.user_id = $1 AND p.specialty_context_id = $2`, "x.patient_id, x.id", "id calculation_code calculation_name created_at formatted_result input_snapshot plain_text_evolution result_interpretation result_unit result_value severity user_id encounter_id patient_id"},
	{"estudios", "patient_medical_studies", `JOIN patients p ON p.id = x.patient_id`, `p.user_id = $1 AND p.specialty_context_id = $2`, "x.patient_id, x.id", "annulled_at version id study_name content_type description file_size original_file_name uploaded_at patient_id"},
	{"recetas-y-ordenes", "patient_prescription_records", `JOIN patients p ON p.id = x.patient_id`, `p.user_id = $1 AND p.specialty_context_id = $2`, "x.patient_id, x.id", "annulled_at version issuance_zone id author_user_id created_at diagnosis_description general_instructions type patient_id"},
	{"items-ordenes", "prescription_diagnostic_orders", `JOIN patient_prescription_records r ON r.id = x.record_id JOIN patients p ON p.id = r.patient_id`, `p.user_id = $1 AND p.specialty_context_id = $2`, "p.id, x.record_id, x.item_position", "record_id study_description item_position"},
	{"items-medicacion", "prescription_medications", `JOIN patient_prescription_records r ON r.id = x.record_id JOIN patients p ON p.id = r.patient_id`, `p.user_id = $1 AND p.specialty_context_id = $2`, "p.id, x.record_id, x.item_position", "record_id administration_route dosage duration_days frequency instructions medication_name item_position"},
	{"vacunas", "patient_vaccinations", `JOIN patients p ON p.id = x.patient_id`, `p.user_id = $1 AND p.specialty_context_id = $2`, "x.patient_id, x.id", "annulled_at version id applied_at dose_id patient_id"},
	{"referencias-dosis", "vaccination_doses", "", `EXISTS (SELECT 1 FROM patient_vaccinations v JOIN patients p ON p.id=v.patient_id WHERE p.user_id = $1 AND p.specialty_context_id = $2 AND v.dose_id=x.id)`, "x.id", "id calendar_id"},
	{"referencias-calendarios", "vaccination_calendars", "", `EXISTS (SELECT 1 FROM patient_vaccinations v JOIN patients p ON p.id=v.patient_id JOIN vaccination_doses d ON d.id=v.dose_id WHERE p.user_id = $1 AND p.specialty_context_id = $2 AND d.calendar_id=x.id)`, "x.id", "id definition"},
	{"revisiones", "clinical_revisions", "", `x.owner_user_id = $1 AND x.specialty_context_id = $2`, "x.patient_id, x.id", "id entity entity_id patient_id workspace_id specialty_context_id owner_user_id actor_user_id occurred_at operation previous_version new_version reason previous_content"},
	{"auditoria-accesos", "clinical_access_events", "", `x.owner_user_id = $1 AND x.specialty_context_id = $2`, "x.id", "id occurred_at actor_user_id actor_kind operation entity_id patient_id record_version owner_user_id specialty_context_id outcome"},
	{"emisiones", "clinical_document_issues", "", `x.owner_user_id = $1 AND x.specialty_context_id = $2`, "x.patient_id, x.id", "id code owner_user_id specialty_context_id patient_id source source_id source_version document_type issuer_specialty issued_at sha256 signature_id signature_name signature_stamp_text signature_sha256"},
	{"anulaciones-documentales", "clinical_document_annulments", `JOIN clinical_document_issues i ON i.id=x.document_id`, `x.owner_user_id = $1 AND x.specialty_context_id = $2 AND i.owner_user_id=$1 AND i.specialty_context_id=$2`, "x.document_id", "document_id owner_user_id specialty_context_id annulled_at reason"},
	{"historial-exportaciones", "clinical_export_evidence", "", `x.owner_user_id = $1 AND x.specialty_context_id = $2`, "x.patient_id, x.id", "id owner_user_id specialty_context_id patient_id prepared_at sha256 manifest"},
}

func (s section) query() string {
	row := "to_jsonb(x)"
	if s.table == "clinical_document_issues" {
		row = "to_jsonb(x) - 'pdf'"
	}
	if s.table == "doctor_signatures" {
		row = "to_jsonb(x) - 'image_png'"
	}
	if s.table == "doctor_clinic_logos" {
		row = "to_jsonb(x) - 'image_webp' - 'image_png'"
	}
	if s.table == "clinical_revisions" {
		row += " || jsonb_build_object('previous_content', x.previous_content - ARRAY['stored_file_name','media_asset_id','media_identity'])"
	}
	// The list of names is an explicit projection. Added schema fields do not
	// enter a copy implicitly. Exact database IDs and numerics are strings.
	parts := make([]string, 0, len(strings.Fields(s.fields)))
	for _, field := range strings.Fields(s.fields) {
		value := "src.row->'" + field + "'"
		if field == "id" || strings.HasSuffix(field, "_id") || field == "version" ||
			strings.HasSuffix(field, "_version") || field == "result_value" ||
			strings.HasSuffix(field, "_kg") || strings.HasSuffix(field, "_cm") {
			value = "src.row->>'" + field + "'"
		}
		parts = append(parts, "'"+field+"', "+value)
	}

	patient := "NULL::bigint"
	switch s.table {
	case "patients":
		patient = "x.id"
	case "prescription_medications", "prescription_diagnostic_orders":
		patient = "p.id"
	case "clinical_document_annulments":
		patient = "i.patient_id"
	case "clinical_encounters", "patient_calculation_logs", "patient_medical_studies", "clinical_access_events",
		"patient_prescription_records", "patient_vaccinations", "clinical_revisions", "clinical_document_issues", "clinical_export_evidence":
		patient = "x.patient_id"
	}
	if s.table == "patient_medical_studies" {
		parts = append(parts, "'archivo', 'adjuntos/estudio-' || x.id::text || CASE x.content_type WHEN 'application/pdf' THEN '.pdf' WHEN 'image/png' THEN '.png' WHEN 'image/jpeg' THEN '.jpg' WHEN 'image/webp' THEN '.webp' WHEN 'image/gif' THEN '.gif' WHEN 'image/bmp' THEN '.bmp' WHEN 'image/tiff' THEN '.tiff' ELSE '.bin' END")
	}
	if s.table == "clinical_document_issues" {
		parts = append(parts, "'archivo', 'documentos/documento-' || x.id::text || '.pdf'")
	}
	return fmt.Sprintf("SELECT jsonb_build_object(%s), %s FROM %s x %s CROSS JOIN LATERAL (SELECT %s AS row) src WHERE %s ORDER BY %s",
		strings.Join(parts, ", "), patient, s.table, s.joins, row, s.where, s.order)
}
