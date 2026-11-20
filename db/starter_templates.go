package db

// StarterTemplate matches the initial wizard and experience authorized by the
// operator. A clinical publication, even if revoked, takes precedence.
// The id argument is an SQL expression from the caller.
func StarterTemplate(id string, requireActive bool) string {
	active := ""
	if requireActive {
		active = " AND starter.active"
	}
	return `EXISTS (
		SELECT 1 FROM public.specialties starter
		JOIN public.wizard_defaults initial ON initial.specialty_id=starter.id
		WHERE starter.id=` + id + active + ` AND initial.published_approval_id IS NULL
		  AND initial.published_version>0
		  AND nullif(current_setting('doctorchart.starter_templates',true),'')::jsonb ->> starter.code =
			encode(sha256(convert_to(public.template_review_content(initial.published_definition,
				public.template_experience(starter.navigation,starter.patient_record_tabs,starter.patient_record_actions))::text,'UTF8')),'hex')
	)`
}

// UsableTemplate is shared by clinician setup and published appointment offers.
func UsableTemplate(id string, requireActive bool) string {
	clinical := "public.template_available(" + id + ")"
	if !requireActive {
		clinical = "public.template_available(" + id + ", false)"
	}
	return "(" + clinical + " OR " + StarterTemplate(id, requireActive) + ")"
}
