-- Esquema inicial consolidado de DoctorChart, versión 16.
-- El bootstrap valida el catálogo y carga los seeds en la misma transacción.

CREATE FUNCTION public.is_iana_timezone(value text) RETURNS boolean
    LANGUAGE sql STABLE STRICT
    SET search_path TO 'pg_catalog'
    AS $$
    SELECT (value = 'UTC' OR position('/' in value) > 0)
       AND value NOT LIKE 'posix/%' AND value NOT LIKE 'right/%'
       AND value NOT LIKE 'Etc/%'
       AND EXISTS (SELECT 1 FROM pg_catalog.pg_timezone_names WHERE name = value)
$$;

CREATE TABLE public.app_schema_state (
    id integer NOT NULL,
    backend text NOT NULL,
    baseline_version integer NOT NULL,
    sql_sha256 character(64) NOT NULL,
    catalog_sha256 character(64) NOT NULL,
    initialized_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT app_schema_state_baseline_version_check CHECK ((baseline_version > 0)),
    CONSTRAINT app_schema_state_id_check CHECK ((id = 1))
);

CREATE TABLE public.booking_confirmation_access (
    booking_id bigint NOT NULL,
    token_hash character(64) NOT NULL,
    encrypted_code bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.booking_events (
    booking_id bigint NOT NULL,
    version bigint NOT NULL,
    kind character varying(24) NOT NULL,
    actor_id character varying(100) NOT NULL,
    occurred_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    before_document jsonb,
    after_document jsonb NOT NULL,
    CONSTRAINT booking_events_actor_id_check CHECK ((length((actor_id)::text) > 0)),
    CONSTRAINT booking_events_after_document_check CHECK ((jsonb_typeof(after_document) = 'object'::text)),
    CONSTRAINT booking_events_kind_check CHECK ((kind IN ('CREATED', 'STATUS_CHANGED', 'RESCHEDULED', 'PATIENT_LINKED'))),
    CONSTRAINT booking_events_version_check CHECK ((version > 0))
);

CREATE TABLE public.booking_idempotency (
    workspace_id bigint NOT NULL,
    key character varying(128) NOT NULL,
    request_sha256 character varying(64) NOT NULL,
    booking_id bigint NOT NULL,
    response_status integer NOT NULL,
    response_document jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT booking_idempotency_key_check CHECK ((length((key)::text) >= 16)),
    CONSTRAINT booking_idempotency_request_sha256_check CHECK (((request_sha256)::text ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT booking_idempotency_response_document_check CHECK ((jsonb_typeof(response_document) = 'object'::text)),
    CONSTRAINT booking_idempotency_response_status_check CHECK ((response_status = ANY (ARRAY[200, 201])))
);

CREATE TABLE public.booking_public_details (
    booking_id bigint NOT NULL,
    reference uuid NOT NULL,
    form_version bigint NOT NULL,
    form_snapshot jsonb NOT NULL,
    answers jsonb NOT NULL,
    document_country character varying(2) NOT NULL,
    document_type character varying(12) NOT NULL,
    document character varying(24) NOT NULL,
    email character varying(254) NOT NULL,
    challenge_hash character varying(64) NOT NULL,
    doctor_name character varying(160) NOT NULL,
    clinic_name character varying(160) NOT NULL,
    address character varying(255) NOT NULL,
    timezone character varying(100) NOT NULL,
    CONSTRAINT booking_public_details_answers_check CHECK ((jsonb_typeof(answers) = 'object'::text)),
    CONSTRAINT booking_public_details_challenge_hash_check CHECK (((challenge_hash)::text ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT booking_public_details_document_type_check CHECK ((document_type IN ('DNI', 'PASSPORT'))),
    CONSTRAINT booking_public_details_form_snapshot_check CHECK ((jsonb_typeof(form_snapshot) = 'object'::text)),
    CONSTRAINT booking_public_details_form_version_check CHECK ((form_version > 0))
);

CREATE TABLE public.booking_publications (
    workspace_id bigint NOT NULL,
    public_id uuid NOT NULL,
    name character varying(160) NOT NULL,
    description character varying(1200) DEFAULT ''::character varying NOT NULL,
    published boolean DEFAULT false NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    questions jsonb DEFAULT '[]'::jsonb NOT NULL,
    CONSTRAINT booking_publications_name_check CHECK ((length(btrim((name)::text)) > 0)),
    CONSTRAINT booking_publications_questions_check CHECK (((jsonb_typeof(questions) = 'array'::text) AND (jsonb_array_length(questions) <= 8))),
    CONSTRAINT booking_publications_version_check CHECK ((version > 0))
);

CREATE TABLE public.bookings (
    id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    clinic_id bigint NOT NULL,
    patient_id bigint,
    start_at timestamp with time zone NOT NULL,
    end_at timestamp with time zone NOT NULL,
    status character varying(20) DEFAULT 'CONFIRMED'::character varying NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    contact_name character varying(160) NOT NULL,
    contact_phone character varying(80) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    updated_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    attendee_name character varying(160),
    specialty_context_id bigint NOT NULL,
    specialty_snapshot jsonb NOT NULL,
    clinic_snapshot jsonb NOT NULL,
    provenance jsonb NOT NULL,
    CONSTRAINT bookings_attendee_name_check CHECK (((attendee_name IS NULL) OR (length(btrim((attendee_name)::text)) > 0))),
    CONSTRAINT bookings_clinic_snapshot_check CHECK (((jsonb_typeof(clinic_snapshot) = 'object'::text) AND (COALESCE((clinic_snapshot ->> 'name'::text), ''::text) <> ''::text) AND (COALESCE((clinic_snapshot ->> 'timezone'::text), ''::text) <> ''::text))),
    CONSTRAINT bookings_contact_name_check CHECK ((length(btrim((contact_name)::text)) > 0)),
    CONSTRAINT bookings_interval_check CHECK ((isfinite(start_at) AND isfinite(end_at) AND (start_at < end_at) AND ((end_at - start_at) <= '08:00:00'::interval))),
    CONSTRAINT bookings_provenance_check CHECK (((jsonb_typeof(provenance) = 'object'::text) AND (COALESCE((provenance ->> 'kind'::text), ''::text) = ANY (ARRAY['LEGACY_CONTEXT_ONLY'::text, 'SCHEDULE_BLOCK'::text])))),
    CONSTRAINT bookings_specialty_snapshot_check CHECK (((jsonb_typeof(specialty_snapshot) = 'object'::text) AND (COALESCE((specialty_snapshot ->> 'id'::text), ''::text) ~ '^[1-9][0-9]*$'::text) AND (COALESCE((specialty_snapshot ->> 'code'::text), ''::text) <> ''::text) AND (COALESCE((specialty_snapshot ->> 'name'::text), ''::text) <> ''::text))),
    CONSTRAINT bookings_status_check CHECK ((status IN ('CONFIRMED', 'CHECKED_IN', 'COMPLETED', 'NO_SHOW', 'CANCELLED'))),
    CONSTRAINT bookings_version_check CHECK ((version > 0))
);

ALTER TABLE public.bookings ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.bookings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.chart_definitions (
    id bigint NOT NULL,
    chart_code character varying(60) NOT NULL,
    chart_type character varying(30) NOT NULL,
    description text,
    is_system boolean NOT NULL,
    required_slots jsonb,
    title character varying(120) NOT NULL
);

ALTER TABLE public.chart_definitions ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.chart_definitions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.clinic_entitlement_epoch (
    id integer NOT NULL,
    generation bigint NOT NULL,
    CONSTRAINT clinic_entitlement_epoch_generation_check CHECK ((generation >= 0)),
    CONSTRAINT clinic_entitlement_epoch_id_check CHECK ((id = 1))
);

CREATE TABLE public.clinic_publications (
    clinic_id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    public_id uuid NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    address character varying(255) NOT NULL,
    modality varchar(16) DEFAULT 'IN_PERSON' NOT NULL,
    CONSTRAINT clinic_publications_modality_address_check CHECK (
        (modality = 'IN_PERSON' AND (NOT enabled OR btrim(address) <> ''))
        OR (modality = 'TELECONSULTATION' AND address = '')
    )
);

CREATE TABLE public.clinic_schedules (
    clinic_id bigint NOT NULL,
    min_notice_minutes integer NOT NULL,
    max_advance_days integer NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    workspace_id bigint NOT NULL,
    CONSTRAINT clinic_schedules_max_advance_days_check CHECK (((max_advance_days >= 1) AND (max_advance_days <= 366))),
    CONSTRAINT clinic_schedules_min_notice_minutes_check CHECK (((min_notice_minutes >= 0) AND (min_notice_minutes <= 525600))),
    CONSTRAINT clinic_schedules_version_check CHECK ((version > 0))
);

CREATE TABLE public.clinic_specialty_contexts (
    clinic_id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    specialty_context_id bigint NOT NULL,
    modality varchar(16) DEFAULT 'IN_PERSON' NOT NULL CHECK (modality = 'IN_PERSON'),
    PRIMARY KEY (clinic_id, specialty_context_id)
);

COMMENT ON TABLE public.clinic_schedules IS 'Local civil agenda configuration; availability is calculated, never materialized.';

CREATE TABLE public.clinical_calculation_definitions (
    id bigint NOT NULL,
    category character varying(60) NOT NULL,
    category_label character varying(60) NOT NULL,
    code character varying(60) NOT NULL,
    description text,
    display_order integer NOT NULL,
    evidence_reference text,
    is_system boolean NOT NULL,
    name character varying(150) NOT NULL,
    required_parameters jsonb,
    result_unit character varying(50),
    short_name character varying(60),
    summary_formula character varying(255)
);

ALTER TABLE public.clinical_calculation_definitions ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.clinical_calculation_definitions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.clinical_encounters (
    id bigint NOT NULL,
    annulled_at timestamp with time zone,
    chief_complaint text,
    clinical_impression text,
    consultation_type character varying(255) NOT NULL,
    diagnostic_orders jsonb,
    encounter_date timestamp with time zone NOT NULL,
    follow_up_date date,
    follow_up_instructions text,
    form_definition_snapshot jsonb NOT NULL,
    form_values jsonb NOT NULL,
    formula_context jsonb NOT NULL,
    formula_results jsonb NOT NULL,
    head_circumference_cm double precision,
    height_cm double precision,
    history_of_present_illness text,
    physical_examination text,
    prescriptions jsonb,
    primary_diagnosis_code character varying(255),
    primary_diagnosis_description character varying(255),
    secondary_diagnoses jsonb,
    treatment_plan text,
    updated_at timestamp(6) with time zone NOT NULL,
    version bigint NOT NULL,
    vital_signs jsonb,
    weight_kg double precision,
    workspace_version bigint NOT NULL,
    patient_id bigint NOT NULL,
    specialty_id bigint NOT NULL,
    CONSTRAINT clinical_encounters_check CHECK (((version >= 0) AND (workspace_version >= 0) AND ((weight_kg IS NULL) OR ((weight_kg > (0)::double precision) AND (weight_kg < 'Infinity'::double precision))) AND ((height_cm IS NULL) OR ((height_cm > (0)::double precision) AND (height_cm < 'Infinity'::double precision))) AND ((head_circumference_cm IS NULL) OR ((head_circumference_cm > (0)::double precision) AND (head_circumference_cm < 'Infinity'::double precision))))),
    CONSTRAINT clinical_encounters_consultation_type_check CHECK (((consultation_type)::text = ANY (ARRAY[('FIRST_VISIT'::character varying)::text, ('FOLLOW_UP'::character varying)::text, ('ROUTINE_CHECKUP'::character varying)::text, ('EMERGENCY'::character varying)::text, ('INTERCONSULTATION'::character varying)::text])))
);

ALTER TABLE public.clinical_encounters ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.clinical_encounters_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.clinics (
    id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    name character varying(160) NOT NULL,
    country character varying(100) NOT NULL,
    province character varying(160) NOT NULL,
    city character varying(160) NOT NULL,
    address character varying(255) NOT NULL,
    timezone character varying(100) NOT NULL,
    status character varying(16) DEFAULT 'ACTIVE'::character varying NOT NULL,
    version bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    modality varchar(16) DEFAULT 'IN_PERSON' NOT NULL,
    CONSTRAINT clinics_modality_location_check CHECK (
        (modality = 'IN_PERSON' AND btrim(country) <> '' AND btrim(province) <> '' AND btrim(city) <> '' AND btrim(address) <> '')
        OR (modality = 'TELECONSULTATION' AND country = '' AND province = '' AND city = '' AND address = '' AND status <> 'ARCHIVED')
    ),
    CONSTRAINT clinics_name_check CHECK ((length(btrim((name)::text)) > 0)),
    CONSTRAINT clinics_status_check CHECK ((status IN ('ACTIVE', 'INACTIVE', 'ARCHIVED'))),
    CONSTRAINT clinics_timestamps_check CHECK ((updated_at >= created_at)),
    CONSTRAINT clinics_timezone_check CHECK (public.is_iana_timezone((timezone)::text)),
    CONSTRAINT clinics_version_check CHECK ((version >= 0))
);

ALTER TABLE public.clinics ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.clinics_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.doctor_calculator_preferences (
    id bigint NOT NULL,
    created_at timestamp(6) with time zone NOT NULL,
    updated_at timestamp(6) with time zone NOT NULL,
    specialty_id bigint NOT NULL,
    workspace_id bigint NOT NULL
);

ALTER TABLE public.doctor_calculator_preferences ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.doctor_calculator_preferences_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.doctor_calculator_selections (
    preference_id bigint NOT NULL,
    calculator_id bigint NOT NULL,
    "position" integer NOT NULL,
    CONSTRAINT doctor_calculator_selections_position_check CHECK (("position" >= 0))
);

CREATE TABLE public.doctor_chart_configs (
    id bigint NOT NULL,
    chart_code character varying(60) NOT NULL,
    field_mappings jsonb,
    is_active boolean NOT NULL,
    user_id character varying(100) NOT NULL
);

ALTER TABLE public.doctor_chart_configs ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.doctor_chart_configs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.doctor_specialty_contexts (
    id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    specialty_id bigint NOT NULL,
    definition jsonb,
    draft_definition jsonb,
    source_wizard_version bigint,
    draft_source_wizard_version bigint,
    version bigint NOT NULL,
    current_step_key character varying(96) NOT NULL,
    onboarding_completed boolean NOT NULL,
    created_at timestamp(6) with time zone NOT NULL,
    updated_at timestamp(6) with time zone NOT NULL,
    teleconsultation_enabled boolean DEFAULT false NOT NULL,
    CONSTRAINT doctor_specialty_contexts_draft_source_wizard_version_check CHECK ((draft_source_wizard_version > 0)),
    CONSTRAINT doctor_specialty_contexts_source_wizard_version_check CHECK ((source_wizard_version > 0)),
    CONSTRAINT doctor_specialty_contexts_version_check CHECK ((version >= 0))
);

ALTER TABLE public.doctor_specialty_contexts ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.doctor_specialty_contexts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.doctor_workspaces (
    id bigint NOT NULL,
    created_at timestamp(6) with time zone NOT NULL,
    patient_columns_config jsonb,
    quick_notes text,
    updated_at timestamp(6) with time zone NOT NULL,
    user_id character varying(100) NOT NULL,
    version bigint NOT NULL,
    CONSTRAINT doctor_workspaces_version_check CHECK ((version >= 0))
);

ALTER TABLE public.doctor_workspaces ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.doctor_workspaces_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.media_assets (
    asset_id uuid NOT NULL,
    document jsonb NOT NULL,
    CONSTRAINT media_assets_asset_id_check CHECK ((((document ->> 'asset_id'::text) = (asset_id)::text) IS TRUE)),
    CONSTRAINT media_assets_service_check CHECK ((((document ->> 'service'::text) = 'doctorchart'::text) IS TRUE)),
    CONSTRAINT media_assets_version_check CHECK ((((document ->> 'version'::text) = '1'::text) IS TRUE))
);

CREATE TABLE public.media_events (
    event_id uuid NOT NULL,
    document jsonb NOT NULL
);

CREATE TABLE public.booking_portal_codes (
    id uuid PRIMARY KEY,
    booking_id bigint UNIQUE,
    subject_hash character(64) NOT NULL,
    email_hash character(64) NOT NULL,
    proof_hash character(64) UNIQUE,
    code_hash character(64) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
    session_hash character(64),
    session_expires_at timestamptz,
    CHECK ((session_hash IS NULL) = (session_expires_at IS NULL))
);

CREATE INDEX booking_portal_codes_email_idx ON public.booking_portal_codes (email_hash, created_at);
CREATE INDEX booking_public_details_portal_idx ON public.booking_public_details (document_country, document_type, document, email);

CREATE TABLE public.notification_outbox (
    id bigint NOT NULL,
    dedup_key character varying(160) NOT NULL,
    kind character varying(32) NOT NULL,
    booking_id bigint,
    booking_version bigint,
    access_id uuid,
    recovery_id uuid,
    payload bytea NOT NULL,
    due_at timestamp with time zone NOT NULL,
    state character varying(16) DEFAULT 'PENDING'::character varying NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    lease_until timestamp with time zone,
    lease_id uuid,
    last_code character varying(48),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    sent_at timestamp with time zone,
    CONSTRAINT notification_outbox_attempts_check CHECK ((attempts >= 0)),
    portal_code_id uuid REFERENCES public.booking_portal_codes(id),
    CONSTRAINT notification_outbox_check CHECK (
        (kind IN ('BOOKING_CONFIRMED','BOOKING_CANCELLED','BOOKING_RESCHEDULED','BOOKING_REMINDER') AND booking_id IS NOT NULL AND booking_version IS NOT NULL AND access_id IS NULL AND recovery_id IS NULL AND portal_code_id IS NULL)
        OR (booking_id IS NULL AND booking_version IS NULL AND (
            (kind='STAFF_INVITATION' AND access_id IS NOT NULL AND recovery_id IS NULL AND portal_code_id IS NULL)
            OR (kind='STAFF_RECOVERY' AND access_id IS NULL AND recovery_id IS NOT NULL AND portal_code_id IS NULL)
            OR (kind='BOOKING_PORTAL_CODE' AND access_id IS NULL AND recovery_id IS NULL AND portal_code_id IS NOT NULL)
        ))
    ),
    CONSTRAINT notification_outbox_state_check CHECK ((state IN ('PENDING', 'PROCESSING', 'SENT', 'OBSOLETE', 'DEAD')))
);

ALTER TABLE public.notification_outbox ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.notification_outbox_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.patient_calculation_logs (
    id bigint NOT NULL,
    calculation_code character varying(60) NOT NULL,
    calculation_name character varying(150) NOT NULL,
    created_at timestamp(6) with time zone NOT NULL,
    formatted_result character varying(100) NOT NULL,
    input_snapshot jsonb NOT NULL,
    plain_text_evolution text,
    result_interpretation text,
    result_unit character varying(50),
    result_value double precision,
    severity character varying(30),
    user_id character varying(100) NOT NULL,
    encounter_id bigint,
    patient_id bigint NOT NULL
);

ALTER TABLE public.patient_calculation_logs ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.patient_calculation_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.patient_medical_studies (
    annulled_at timestamp with time zone,
    version bigint DEFAULT 0 NOT NULL CHECK (version >= 0),
    id bigint NOT NULL,
    study_name character varying(240) NOT NULL,
    media_asset_id uuid,
    content_type character varying(255) NOT NULL,
    description text,
    file_size bigint NOT NULL,
    original_file_name character varying(255) NOT NULL,
    stored_file_name character varying(36) NOT NULL,
    uploaded_at timestamp with time zone NOT NULL,
    patient_id bigint NOT NULL,
    CONSTRAINT patient_medical_studies_study_name_check CHECK ((btrim((study_name)::text) <> ''::text))
);

ALTER TABLE public.patient_medical_studies ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.patient_medical_studies_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.patient_prescription_records (
    annulled_at timestamp with time zone,
    version bigint DEFAULT 0 NOT NULL CHECK (version >= 0),
    issuance_zone character varying(100) DEFAULT 'America/Argentina/Buenos_Aires'::character varying NOT NULL,
    id bigint NOT NULL,
    author_user_id character varying(100) NOT NULL,
    created_at timestamp(6) with time zone NOT NULL,
    diagnosis_description character varying(4000) NOT NULL,
    general_instructions character varying(4000) NOT NULL,
    type character varying(20) NOT NULL,
    patient_id bigint NOT NULL,
    CONSTRAINT patient_prescription_records_type_check CHECK (((type)::text = ANY (ARRAY[('PRESCRIPTION'::character varying)::text, ('STUDY_ORDERS'::character varying)::text])))
);

ALTER TABLE public.patient_prescription_records ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.patient_prescription_records_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.patient_vaccinations (
    annulled_at timestamp with time zone,
    version bigint DEFAULT 0 NOT NULL CHECK (version >= 0),
    id bigint NOT NULL,
    applied_at date NOT NULL,
    dose_id character varying(80) NOT NULL,
    patient_id bigint NOT NULL
);

ALTER TABLE public.patient_vaccinations ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.patient_vaccinations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.patients (
    id bigint NOT NULL,
    annulled_at timestamp with time zone,
    media_identity uuid DEFAULT gen_random_uuid() NOT NULL,
    address character varying(255),
    allergies jsonb,
    birth_date date NOT NULL,
    blood_type character varying(255),
    chronic_conditions jsonb,
    email character varying(255),
    family_history jsonb,
    form_definition_snapshot jsonb NOT NULL,
    form_values jsonb NOT NULL,
    formula_context jsonb NOT NULL,
    formula_results jsonb NOT NULL,
    full_name character varying(255) NOT NULL,
    gender character varying(255),
    guardian_info jsonb,
    insurance_info jsonb,
    national_id character varying(255),
    personal_history jsonb,
    phone character varying(255),
    updated_at timestamp(6) with time zone NOT NULL,
    user_id character varying(100) NOT NULL,
    version bigint NOT NULL,
    workspace_version bigint NOT NULL,
    workspace_id bigint NOT NULL,
    specialty_context_id bigint NOT NULL,
    CONSTRAINT patients_check CHECK (((version >= 0) AND (workspace_version >= 0))),
    CONSTRAINT patients_gender_check CHECK (((gender)::text = ANY (ARRAY[('MALE'::character varying)::text, ('FEMALE'::character varying)::text, ('OTHER'::character varying)::text, ('UNKNOWN'::character varying)::text])))
);

ALTER TABLE public.patients ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.patients_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.prescription_diagnostic_orders (
    record_id bigint NOT NULL,
    study_description character varying(2000) NOT NULL,
    item_position integer NOT NULL,
    CONSTRAINT prescription_diagnostic_orders_item_position_check CHECK ((item_position >= 0))
);

CREATE TABLE public.prescription_medications (
    record_id bigint NOT NULL,
    administration_route character varying(200) NOT NULL,
    dosage character varying(500) NOT NULL,
    duration_days integer,
    frequency character varying(200) NOT NULL,
    instructions character varying(4000) NOT NULL,
    medication_name character varying(200) NOT NULL,
    item_position integer NOT NULL,
    CONSTRAINT prescription_medications_item_position_check CHECK ((item_position >= 0))
);

CREATE TABLE public.public_booking_rate_windows (
    subject character varying(64) NOT NULL,
    kind character varying(16) NOT NULL,
    minute bigint NOT NULL,
    used integer NOT NULL,
    CONSTRAINT public_booking_rate_windows_subject_check CHECK (((subject)::text ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT public_booking_rate_windows_used_check CHECK ((used > 0))
);

CREATE TABLE public.publication_specialty_contexts (
    workspace_id bigint NOT NULL,
    specialty_context_id bigint NOT NULL
);

CREATE TABLE public.schedule_blocks (
    id bigint NOT NULL,
    clinic_id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    specialty_context_id bigint NOT NULL,
    weekday integer,
    exception_id bigint,
    start_minute integer NOT NULL,
    end_minute integer NOT NULL,
    slot_minutes integer NOT NULL,
    CONSTRAINT schedule_blocks_check CHECK (end_minute BETWEEN 1 AND 1440 AND end_minute > start_minute),
    CONSTRAINT schedule_blocks_kind_check CHECK (((weekday IS NULL) <> (exception_id IS NULL))),
    CONSTRAINT schedule_blocks_slot_minutes_check CHECK (((slot_minutes >= 5) AND (slot_minutes <= 480))),
    CONSTRAINT schedule_blocks_start_minute_check CHECK (((start_minute >= 0) AND (start_minute <= 1439))),
    CONSTRAINT schedule_blocks_weekday_check CHECK (((weekday >= 1) AND (weekday <= 7)))
);

ALTER TABLE public.schedule_blocks ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.schedule_blocks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.schedule_exceptions (
    id bigint NOT NULL,
    clinic_id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    from_date date NOT NULL,
    through_date date NOT NULL,
    label text NOT NULL,
    CONSTRAINT schedule_exceptions_check CHECK (((through_date >= from_date) AND (through_date <= '9999-12-31'::date))),
    CONSTRAINT schedule_exceptions_from_date_check CHECK (((from_date >= '0001-01-01'::date) AND (from_date <= '9999-12-31'::date)))
);

ALTER TABLE public.schedule_exceptions ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.schedule_exceptions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.specialties (
    id bigint NOT NULL,
    active boolean NOT NULL,
    code character varying(80) NOT NULL,
    created_at timestamp(6) with time zone NOT NULL,
    description text,
    display_name character varying(120) NOT NULL,
    navigation jsonb NOT NULL,
    patient_record_actions jsonb NOT NULL,
    patient_record_tabs jsonb NOT NULL,
    updated_at timestamp(6) with time zone NOT NULL,
    version bigint NOT NULL,
    CONSTRAINT specialties_version_check CHECK ((version >= 0)),
    CONSTRAINT specialties_version_check1 CHECK ((version >= 0))
);

ALTER TABLE public.specialties ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.specialties_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.specialty_calculator_defaults (
    id bigint NOT NULL,
    "position" integer NOT NULL,
    calculator_id bigint NOT NULL,
    specialty_id bigint NOT NULL,
    CONSTRAINT specialty_calculator_defaults_position_check CHECK (("position" >= 0))
);

ALTER TABLE public.specialty_calculator_defaults ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.specialty_calculator_defaults_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.specialty_context_preferences (
    specialty_context_id bigint NOT NULL,
    version bigint DEFAULT 0 NOT NULL,
    patient_columns_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    quick_notes text,
    CONSTRAINT specialty_context_preferences_patient_columns_config_check CHECK ((jsonb_typeof(patient_columns_config) = 'object'::text)),
    CONSTRAINT specialty_context_preferences_version_check CHECK ((version >= 0))
);

CREATE TABLE public.staff_access (
    id uuid NOT NULL,
    workspace_id bigint NOT NULL,
    account_id uuid,
    email character varying(254) NOT NULL,
    name character varying(160) NOT NULL,
    phone character varying(16) DEFAULT '' NOT NULL,
    permissions text[] NOT NULL,
    active boolean DEFAULT true NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    CONSTRAINT staff_access_email_check CHECK (((email)::text = lower(btrim((email)::text)))),
    CONSTRAINT staff_access_name_check CHECK ((length((name)::text) > 0)),
    CONSTRAINT staff_access_phone_check CHECK (phone = '' OR phone ~ '^\+[1-9][0-9]{7,14}$'),
    CONSTRAINT staff_access_permissions_check CHECK (((permissions <@ ARRAY['view'::text, 'create'::text, 'cancel'::text, 'reschedule'::text, 'status'::text]) AND ('view'::text = ANY (permissions)))),
    CONSTRAINT staff_access_version_check CHECK ((version > 0))
);

CREATE TABLE public.staff_access_clinics (
    access_id uuid NOT NULL,
    workspace_id bigint NOT NULL,
    clinic_id bigint NOT NULL
);

CREATE TABLE public.staff_accounts (
    id uuid NOT NULL,
    email character varying(254) NOT NULL,
    name character varying(160) NOT NULL,
    password_hash text NOT NULL,
    recovery_hash character(64) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT staff_accounts_email_check CHECK (((email)::text = lower(btrim((email)::text)))),
    CONSTRAINT staff_accounts_name_check CHECK ((length((name)::text) > 0))
);

CREATE TABLE public.staff_email_recovery (
    id uuid NOT NULL,
    account_id uuid NOT NULL,
    token_hash character(64) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.staff_invitations (
    token_hash character(64) NOT NULL,
    access_id uuid NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.staff_rate_windows (
    key character(64) NOT NULL,
    bucket timestamp with time zone NOT NULL,
    attempts integer NOT NULL,
    CONSTRAINT staff_rate_windows_attempts_check CHECK ((attempts > 0))
);

CREATE TABLE public.staff_security_events (
    id bigint NOT NULL,
    account_id uuid,
    access_id uuid,
    kind character varying(48) NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE public.staff_security_events ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.staff_security_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.staff_sessions (
    token_hash character(64) NOT NULL,
    account_id uuid NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.study_file_deletions (
    storage_key character varying(36) NOT NULL,
    requested_at timestamp(6) with time zone NOT NULL
);

CREATE TABLE public.vaccination_calendars (
    id character varying(80) NOT NULL,
    definition jsonb NOT NULL
);

CREATE TABLE public.vaccination_doses (
    id character varying(80) NOT NULL,
    calendar_id character varying(80) NOT NULL
);

CREATE TABLE public.wizard_defaults (
    draft_experience jsonb,
    published_approval_id bigint,
    id bigint NOT NULL,
    draft_definition jsonb NOT NULL,
    published_definition jsonb,
    published_version bigint NOT NULL,
    updated_at timestamp(6) with time zone NOT NULL,
    version bigint NOT NULL,
    specialty_id bigint NOT NULL,
    CONSTRAINT wizard_defaults_check CHECK (((version >= 0) AND (published_version >= 0) AND (((published_definition IS NULL) AND (published_version = 0)) OR ((published_definition IS NOT NULL) AND (published_version > 0))))),
    CONSTRAINT wizard_defaults_published_version_check CHECK ((published_version >= 0)),
    CONSTRAINT wizard_defaults_version_check CHECK ((version >= 0))
);

ALTER TABLE public.wizard_defaults ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.wizard_defaults_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

ALTER TABLE ONLY public.app_schema_state
    ADD CONSTRAINT app_schema_state_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.booking_confirmation_access
    ADD CONSTRAINT booking_confirmation_access_pkey PRIMARY KEY (booking_id);

ALTER TABLE ONLY public.booking_confirmation_access
    ADD CONSTRAINT booking_confirmation_access_token_hash_key UNIQUE (token_hash);

ALTER TABLE ONLY public.booking_events
    ADD CONSTRAINT booking_events_pkey PRIMARY KEY (booking_id, version);

ALTER TABLE ONLY public.booking_idempotency
    ADD CONSTRAINT booking_idempotency_pkey PRIMARY KEY (workspace_id, key);

ALTER TABLE ONLY public.booking_public_details
    ADD CONSTRAINT booking_public_details_challenge_hash_key UNIQUE (challenge_hash);

ALTER TABLE ONLY public.booking_public_details
    ADD CONSTRAINT booking_public_details_pkey PRIMARY KEY (booking_id);

ALTER TABLE ONLY public.booking_public_details
    ADD CONSTRAINT booking_public_details_reference_key UNIQUE (reference);

ALTER TABLE ONLY public.booking_publications
    ADD CONSTRAINT booking_publications_pkey PRIMARY KEY (workspace_id);

ALTER TABLE ONLY public.booking_publications
    ADD CONSTRAINT booking_publications_public_id_key UNIQUE (public_id);

ALTER TABLE ONLY public.bookings
    ADD CONSTRAINT bookings_owner_interval_exclusion EXCLUDE USING gist (numrange((workspace_id)::numeric, (workspace_id)::numeric, '[]'::text) WITH &&, tstzrange(start_at, end_at, '[)'::text) WITH &&) WHERE (((status)::text <> 'CANCELLED'::text));

ALTER TABLE ONLY public.bookings
    ADD CONSTRAINT bookings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.chart_definitions
    ADD CONSTRAINT chart_definitions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.clinic_entitlement_epoch
    ADD CONSTRAINT clinic_entitlement_epoch_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.clinic_publications
    ADD CONSTRAINT clinic_publications_pkey PRIMARY KEY (clinic_id);

ALTER TABLE ONLY public.clinic_publications
    ADD CONSTRAINT clinic_publications_public_id_key UNIQUE (public_id);

ALTER TABLE ONLY public.clinic_schedules
    ADD CONSTRAINT clinic_schedules_clinic_workspace_key UNIQUE (clinic_id, workspace_id);

ALTER TABLE ONLY public.clinic_schedules
    ADD CONSTRAINT clinic_schedules_pkey PRIMARY KEY (clinic_id);

ALTER TABLE ONLY public.clinical_calculation_definitions
    ADD CONSTRAINT clinical_calculation_definitions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.clinical_encounters
    ADD CONSTRAINT clinical_encounters_id_patient_key UNIQUE (id, patient_id);

ALTER TABLE ONLY public.clinical_encounters
    ADD CONSTRAINT clinical_encounters_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.clinics
    ADD CONSTRAINT clinics_id_workspace_key UNIQUE (id, workspace_id);

ALTER TABLE ONLY public.clinics
    ADD CONSTRAINT clinics_id_workspace_unique UNIQUE (id, workspace_id);

ALTER TABLE ONLY public.clinics
    ADD CONSTRAINT clinics_id_workspace_modality_key UNIQUE (id, workspace_id, modality);

ALTER TABLE ONLY public.clinics
    ADD CONSTRAINT clinics_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.doctor_calculator_preferences
    ADD CONSTRAINT doctor_calculator_preferences_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.doctor_calculator_selections
    ADD CONSTRAINT doctor_calculator_selections_pkey PRIMARY KEY (preference_id, "position");

ALTER TABLE ONLY public.doctor_chart_configs
    ADD CONSTRAINT doctor_chart_configs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.doctor_specialty_contexts
    ADD CONSTRAINT doctor_specialty_contexts_id_workspace_key UNIQUE (id, workspace_id);

ALTER TABLE ONLY public.doctor_specialty_contexts
    ADD CONSTRAINT doctor_specialty_contexts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.doctor_specialty_contexts
    ADD CONSTRAINT doctor_specialty_contexts_workspace_specialty_key UNIQUE (workspace_id, specialty_id);

ALTER TABLE ONLY public.doctor_workspaces
    ADD CONSTRAINT doctor_workspaces_id_owner_key UNIQUE (id, user_id);

ALTER TABLE ONLY public.doctor_workspaces
    ADD CONSTRAINT doctor_workspaces_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.media_assets
    ADD CONSTRAINT media_assets_pkey PRIMARY KEY (asset_id);

ALTER TABLE ONLY public.media_events
    ADD CONSTRAINT media_events_pkey PRIMARY KEY (event_id);

ALTER TABLE ONLY public.notification_outbox
    ADD CONSTRAINT notification_outbox_dedup_key_key UNIQUE (dedup_key);

ALTER TABLE ONLY public.notification_outbox
    ADD CONSTRAINT notification_outbox_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.patient_calculation_logs
    ADD CONSTRAINT patient_calculation_logs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.patient_medical_studies
    ADD CONSTRAINT patient_medical_studies_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.patient_prescription_records
    ADD CONSTRAINT patient_prescription_records_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.patient_vaccinations
    ADD CONSTRAINT patient_vaccinations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.patients
    ADD CONSTRAINT patients_id_owner_key UNIQUE (id, user_id);

ALTER TABLE ONLY public.patients
    ADD CONSTRAINT patients_id_workspace_context_key UNIQUE (id, workspace_id, specialty_context_id);

ALTER TABLE ONLY public.patients
    ADD CONSTRAINT patients_media_identity_key UNIQUE (media_identity);

ALTER TABLE ONLY public.patients
    ADD CONSTRAINT patients_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.prescription_diagnostic_orders
    ADD CONSTRAINT prescription_diagnostic_orders_pkey PRIMARY KEY (record_id, item_position);

ALTER TABLE ONLY public.prescription_medications
    ADD CONSTRAINT prescription_medications_pkey PRIMARY KEY (record_id, item_position);

ALTER TABLE ONLY public.public_booking_rate_windows
    ADD CONSTRAINT public_booking_rate_windows_pkey PRIMARY KEY (subject, kind, minute);

ALTER TABLE ONLY public.publication_specialty_contexts
    ADD CONSTRAINT publication_specialty_contexts_pkey PRIMARY KEY (workspace_id, specialty_context_id);

ALTER TABLE ONLY public.schedule_blocks
    ADD CONSTRAINT schedule_blocks_exception_no_overlap EXCLUDE USING gist (int8range(exception_id, exception_id, '[]'::text) WITH &&, int4range(start_minute, end_minute, '[)'::text) WITH &&) WHERE ((exception_id IS NOT NULL)) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY public.schedule_blocks
    ADD CONSTRAINT schedule_blocks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.schedule_blocks
    ADD CONSTRAINT schedule_blocks_weekly_no_overlap EXCLUDE USING gist (int8range(clinic_id, clinic_id, '[]'::text) WITH &&, int4range(weekday, weekday, '[]'::text) WITH &&, int4range(start_minute, end_minute, '[)'::text) WITH &&) WHERE ((exception_id IS NULL)) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY public.schedule_exceptions
    ADD CONSTRAINT schedule_exceptions_id_clinic_workspace_key UNIQUE (id, clinic_id, workspace_id);

ALTER TABLE ONLY public.schedule_exceptions
    ADD CONSTRAINT schedule_exceptions_no_overlap EXCLUDE USING gist (int8range(clinic_id, clinic_id, '[]'::text) WITH &&, daterange(from_date, through_date, '[]'::text) WITH &&) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY public.schedule_exceptions
    ADD CONSTRAINT schedule_exceptions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.specialties
    ADD CONSTRAINT specialties_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.specialty_calculator_defaults
    ADD CONSTRAINT specialty_calculator_defaults_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.specialty_context_preferences
    ADD CONSTRAINT specialty_context_preferences_pkey PRIMARY KEY (specialty_context_id);

ALTER TABLE ONLY public.staff_access_clinics
    ADD CONSTRAINT staff_access_clinics_pkey PRIMARY KEY (access_id, clinic_id);

ALTER TABLE ONLY public.staff_access
    ADD CONSTRAINT staff_access_id_workspace_id_key UNIQUE (id, workspace_id);

ALTER TABLE ONLY public.staff_access
    ADD CONSTRAINT staff_access_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.staff_access
    ADD CONSTRAINT staff_access_workspace_id_email_key UNIQUE (workspace_id, email);

ALTER TABLE ONLY public.staff_accounts
    ADD CONSTRAINT staff_accounts_email_key UNIQUE (email);

ALTER TABLE ONLY public.staff_accounts
    ADD CONSTRAINT staff_accounts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.staff_email_recovery
    ADD CONSTRAINT staff_email_recovery_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.staff_email_recovery
    ADD CONSTRAINT staff_email_recovery_token_hash_key UNIQUE (token_hash);

ALTER TABLE ONLY public.staff_invitations
    ADD CONSTRAINT staff_invitations_pkey PRIMARY KEY (token_hash);

ALTER TABLE ONLY public.staff_rate_windows
    ADD CONSTRAINT staff_rate_windows_pkey PRIMARY KEY (key, bucket);

ALTER TABLE ONLY public.staff_security_events
    ADD CONSTRAINT staff_security_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.staff_sessions
    ADD CONSTRAINT staff_sessions_pkey PRIMARY KEY (token_hash);

ALTER TABLE ONLY public.study_file_deletions
    ADD CONSTRAINT study_file_deletions_pkey PRIMARY KEY (storage_key);

ALTER TABLE ONLY public.wizard_defaults
    ADD CONSTRAINT uk522n4qh2430n1xgtirsj7w274 UNIQUE (specialty_id);

ALTER TABLE ONLY public.specialties
    ADD CONSTRAINT uk8hq46e4ylgbhk67ke0u9y52i3 UNIQUE (code);

ALTER TABLE ONLY public.patient_medical_studies
    ADD CONSTRAINT uk8u0lt9ap874e8jtlw4rcfft5g UNIQUE (stored_file_name);

ALTER TABLE ONLY public.doctor_chart_configs
    ADD CONSTRAINT uk_doctor_chart_user_code UNIQUE (user_id, chart_code);

CREATE UNIQUE INDEX uk_patient_vaccination_dose ON public.patient_vaccinations (patient_id, dose_id) WHERE annulled_at IS NULL;

ALTER TABLE ONLY public.chart_definitions
    ADD CONSTRAINT ukbkgqjqvmt01knb5i9fek29k9k UNIQUE (chart_code);

ALTER TABLE ONLY public.specialty_calculator_defaults
    ADD CONSTRAINT ukgf329gc9ulckccki6rid0l7wc UNIQUE (specialty_id, calculator_id);

ALTER TABLE ONLY public.doctor_calculator_selections
    ADD CONSTRAINT ukj99s9047freh6m013mknom50b UNIQUE (preference_id, calculator_id);

ALTER TABLE ONLY public.doctor_calculator_preferences
    ADD CONSTRAINT ukmif2gm69vcu90puvnk7390wyb UNIQUE (workspace_id, specialty_id);

ALTER TABLE ONLY public.doctor_workspaces
    ADD CONSTRAINT uknawal5i41ucqalejtg9s6se4y UNIQUE (user_id);

ALTER TABLE ONLY public.specialty_calculator_defaults
    ADD CONSTRAINT uknidv1oblrfhp4jv2vae4fma5y UNIQUE (specialty_id, "position");

ALTER TABLE ONLY public.clinical_calculation_definitions
    ADD CONSTRAINT ukpvl043f89txrudu92gg2rsnbc UNIQUE (code);

ALTER TABLE ONLY public.vaccination_calendars
    ADD CONSTRAINT vaccination_calendars_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.vaccination_doses
    ADD CONSTRAINT vaccination_doses_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.wizard_defaults
    ADD CONSTRAINT wizard_defaults_pkey PRIMARY KEY (id);

CREATE INDEX bookings_context_start_idx ON public.bookings USING btree (specialty_context_id, start_at, id);

CREATE INDEX bookings_patient_idx ON public.bookings USING btree (patient_id) WHERE (patient_id IS NOT NULL);

CREATE INDEX bookings_workspace_start_idx ON public.bookings USING btree (workspace_id, start_at, id);

CREATE UNIQUE INDEX clinical_encounters_one_first_visit ON public.clinical_encounters USING btree (patient_id) WHERE annulled_at IS NULL AND consultation_type = 'FIRST_VISIT';

CREATE UNIQUE INDEX clinics_one_virtual_idx ON public.clinics (workspace_id) WHERE modality = 'TELECONSULTATION';

CREATE INDEX clinics_workspace_status_idx ON public.clinics USING btree (workspace_id, status, id);

CREATE INDEX idx_encounters_patient_id ON public.clinical_encounters USING btree (patient_id);

CREATE INDEX idx_patients_user_id ON public.patients USING btree (user_id);

CREATE UNIQUE INDEX patients_active_national_id_idx ON public.patients (user_id, specialty_context_id, national_id) WHERE annulled_at IS NULL AND national_id IS NOT NULL;

CREATE INDEX idx_prescription_patient_date ON public.patient_prescription_records USING btree (patient_id, created_at, id);

CREATE INDEX notification_outbox_booking_idx ON public.notification_outbox USING btree (booking_id, booking_version);

CREATE INDEX notification_outbox_due_idx ON public.notification_outbox USING btree (due_at, id) WHERE (state IN ('PENDING', 'PROCESSING'));

CREATE INDEX patients_context_id_idx ON public.patients USING btree (specialty_context_id, id);

CREATE INDEX patients_context_name_id_idx ON public.patients USING btree (specialty_context_id, full_name, id);

CREATE INDEX public_booking_rate_expiry_idx ON public.public_booking_rate_windows USING btree (minute);

CREATE INDEX schedule_blocks_clinic_idx ON public.schedule_blocks USING btree (clinic_id, weekday, start_minute, id);

CREATE INDEX schedule_blocks_context_idx ON public.schedule_blocks USING btree (specialty_context_id, id);

CREATE INDEX schedule_blocks_exception_idx ON public.schedule_blocks USING btree (exception_id);

CREATE INDEX staff_access_account_idx ON public.staff_access USING btree (account_id) WHERE active;

CREATE INDEX staff_email_recovery_account_idx ON public.staff_email_recovery USING btree (account_id);

CREATE INDEX staff_invitations_access_idx ON public.staff_invitations USING btree (access_id);

CREATE INDEX staff_security_events_account_idx ON public.staff_security_events USING btree (account_id, occurred_at);

CREATE INDEX staff_sessions_account_idx ON public.staff_sessions USING btree (account_id);

CREATE FUNCTION public.booking_block_snapshot(block bigint) RETURNS jsonb
    LANGUAGE sql STABLE
    SET search_path TO 'pg_catalog'
    AS $$
 SELECT jsonb_build_object('kind','SCHEDULE_BLOCK','blockId',b.id::text,'scheduleVersion',s.version::text,
  'weekday',COALESCE(b.weekday,0),'exceptionId',COALESCE(b.exception_id::text,''),
  'from',COALESCE(e.from_date::text,''),'through',COALESCE(e.through_date::text,''),
  'startMinute',b.start_minute,'endMinute',b.end_minute,'slotMinutes',b.slot_minutes,'timezone',c.timezone)
 FROM public.schedule_blocks b JOIN public.clinic_schedules s ON s.clinic_id=b.clinic_id
 JOIN public.clinics c ON c.id=b.clinic_id LEFT JOIN public.schedule_exceptions e ON e.id=b.exception_id WHERE b.id=block
$$;

CREATE FUNCTION public.booking_clinic_snapshot(clinic bigint) RETURNS jsonb
    LANGUAGE sql STABLE
    SET search_path TO 'pg_catalog'
    AS $$
 SELECT jsonb_build_object('name',c.name,'address',c.address,'publicAddress',COALESCE(p.address,''),'timezone',c.timezone,'modality',c.modality)
 FROM public.clinics c LEFT JOIN public.clinic_publications p ON p.clinic_id=c.id AND p.enabled WHERE c.id=clinic
$$;

CREATE FUNCTION public.guard_booking_context_readiness() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path TO 'pg_catalog'
    AS $$
BEGIN
 IF TG_OP='INSERT' OR (NEW.start_at,NEW.end_at,NEW.clinic_id,NEW.provenance) IS DISTINCT FROM (OLD.start_at,OLD.end_at,OLD.clinic_id,OLD.provenance) THEN
  PERFORM id FROM public.doctor_specialty_contexts WHERE id=NEW.specialty_context_id AND workspace_id=NEW.workspace_id AND onboarding_completed AND definition IS NOT NULL FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'booking context is not ready' USING ERRCODE='23514'; END IF;
 END IF;RETURN NEW;
END $$;

CREATE FUNCTION public.guard_clinical_patient_binding() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF NEW.patient_id<>OLD.patient_id THEN
  RAISE EXCEPTION 'Clinical record patient is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION public.guard_context_binding() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
BEGIN
 IF NEW.workspace_id<>OLD.workspace_id OR (NEW.specialty_id<>OLD.specialty_id AND
  (EXISTS(SELECT 1 FROM public.patients WHERE specialty_context_id=OLD.id) OR EXISTS(SELECT 1 FROM public.bookings WHERE specialty_context_id=OLD.id))) THEN
  RAISE EXCEPTION 'Clinical context binding is immutable once used' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION public.guard_encounter_specialty() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM public.patients p JOIN public.doctor_specialty_contexts c ON c.id=p.specialty_context_id
               WHERE p.id=NEW.patient_id AND c.specialty_id=NEW.specialty_id) THEN
  RAISE EXCEPTION 'Encounter specialty contradicts patient context' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION public.guard_patient_context() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF TG_OP='UPDATE' AND (NEW.specialty_context_id,NEW.workspace_id,NEW.user_id) IS DISTINCT FROM (OLD.specialty_context_id,OLD.workspace_id,OLD.user_id) THEN
  RAISE EXCEPTION 'Patient clinical ownership is immutable' USING ERRCODE='23514';
 END IF;
 -- Serialize inserts with specialty edits; FK checks separately enforce owner.
 PERFORM id FROM public.doctor_specialty_contexts WHERE id=NEW.specialty_context_id FOR SHARE;
 RETURN NEW;
END $$;

CREATE FUNCTION public.initialize_context_preferences() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path TO 'pg_catalog'
    AS $$
BEGIN INSERT INTO public.specialty_context_preferences(specialty_context_id) VALUES(NEW.id); RETURN NEW; END $$;

CREATE FUNCTION public.validate_booking_attendee() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path TO 'pg_catalog'
    AS $$
BEGIN
 IF NEW.attendee_name IS DISTINCT FROM OLD.attendee_name THEN
  RAISE EXCEPTION 'booking attendee is immutable' USING ERRCODE='23514',CONSTRAINT='bookings_attendee_immutable';
 END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION public.validate_booking_owner_transition() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $_$
DECLARE source public.schedule_blocks%ROWTYPE; zone text; local_start timestamp; minute integer; moving boolean;
BEGIN
 PERFORM id FROM public.doctor_specialty_contexts WHERE id=NEW.specialty_context_id FOR SHARE;
 IF NEW.patient_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM public.patients p WHERE p.id=NEW.patient_id AND p.workspace_id=NEW.workspace_id AND p.specialty_context_id=NEW.specialty_context_id) THEN
  RAISE EXCEPTION 'booking patient context mismatch' USING ERRCODE='23514',CONSTRAINT='bookings_patient_context_fkey';
 END IF;
 IF NEW.patient_id IS NOT NULL AND (TG_OP='INSERT' OR OLD.patient_id IS DISTINCT FROM NEW.patient_id) THEN
  PERFORM 1 FROM public.patients p WHERE p.id=NEW.patient_id AND p.annulled_at IS NULL FOR SHARE;
  IF NOT FOUND THEN
   RAISE EXCEPTION 'booking requires an active patient' USING ERRCODE='23514',CONSTRAINT='bookings_patient_active_check';
  END IF;
 END IF;
 moving:=TG_OP='INSERT';
 IF TG_OP='UPDATE' THEN
  IF NEW.id<>OLD.id OR NEW.workspace_id<>OLD.workspace_id OR NEW.created_at<>OLD.created_at OR NEW.version<>OLD.version+1 THEN
   RAISE EXCEPTION 'booking immutable identity or version mismatch' USING ERRCODE='23514',CONSTRAINT='bookings_identity_version_check';
  END IF;
  IF NEW.specialty_context_id<>OLD.specialty_context_id OR NEW.specialty_snapshot IS DISTINCT FROM OLD.specialty_snapshot THEN
   RAISE EXCEPTION 'booking specialty is immutable' USING ERRCODE='23514',CONSTRAINT='bookings_context_immutable';
  END IF;
  IF NEW.contact_name<>OLD.contact_name OR NEW.contact_phone<>OLD.contact_phone OR
     (NEW.patient_id IS DISTINCT FROM OLD.patient_id AND (OLD.patient_id IS NOT NULL OR NEW.patient_id IS NULL)) THEN
   RAISE EXCEPTION 'booking contact or existing patient link is immutable' USING ERRCODE='23514',CONSTRAINT='bookings_contact_check';
  END IF;
  moving:=(NEW.start_at,NEW.end_at,NEW.clinic_id) IS DISTINCT FROM (OLD.start_at,OLD.end_at,OLD.clinic_id) OR NEW.provenance IS DISTINCT FROM OLD.provenance OR NEW.clinic_snapshot IS DISTINCT FROM OLD.clinic_snapshot;
  IF moving AND (OLD.status<>'CONFIRMED' OR NEW.status<>'CONFIRMED' OR NEW.patient_id IS DISTINCT FROM OLD.patient_id) THEN
   RAISE EXCEPTION 'booking cannot be rescheduled in current state' USING ERRCODE='23514',CONSTRAINT='bookings_transition_check';
  END IF;
  IF NEW.status<>OLD.status AND NOT ((OLD.status='CONFIRMED' AND NEW.status IN ('CHECKED_IN','NO_SHOW','CANCELLED')) OR (OLD.status='CHECKED_IN' AND NEW.status IN ('COMPLETED','CANCELLED'))) THEN
   RAISE EXCEPTION 'invalid booking status transition' USING ERRCODE='23514',CONSTRAINT='bookings_transition_check';
  END IF;
 ELSE
  IF NEW.specialty_snapshot IS DISTINCT FROM (SELECT jsonb_build_object('id',s.id::text,'code',s.code,'name',s.display_name) FROM public.doctor_specialty_contexts c JOIN public.specialties s ON s.id=c.specialty_id WHERE c.id=NEW.specialty_context_id AND c.workspace_id=NEW.workspace_id) THEN
   RAISE EXCEPTION 'booking specialty snapshot mismatch' USING ERRCODE='23514';
  END IF;
 END IF;
 IF moving THEN
  IF NEW.provenance->>'kind' IS DISTINCT FROM 'SCHEDULE_BLOCK' OR COALESCE(NEW.provenance->>'blockId','') !~ '^[1-9][0-9]*$' THEN
   RAISE EXCEPTION 'new/rescheduled booking requires exact block provenance' USING ERRCODE='23514';
  END IF;
  SELECT * INTO source FROM public.schedule_blocks b WHERE b.id=(NEW.provenance->>'blockId')::bigint AND b.clinic_id=NEW.clinic_id AND b.workspace_id=NEW.workspace_id AND b.specialty_context_id=NEW.specialty_context_id FOR SHARE;
  IF NOT FOUND OR NEW.provenance IS DISTINCT FROM public.booking_block_snapshot(source.id) OR NEW.clinic_snapshot IS DISTINCT FROM public.booking_clinic_snapshot(NEW.clinic_id) THEN
   RAISE EXCEPTION 'booking block provenance mismatch' USING ERRCODE='23514';
  END IF;
  zone:=NEW.provenance->>'timezone'; local_start:=NEW.start_at AT TIME ZONE zone;
  minute:=extract(hour FROM local_start)::int*60+extract(minute FROM local_start)::int;
  IF extract(second FROM local_start)<>0 OR minute<source.start_minute OR minute+source.slot_minutes>source.end_minute OR (minute-source.start_minute)%source.slot_minutes<>0 OR NEW.end_at-NEW.start_at<>make_interval(mins=>source.slot_minutes) THEN
   RAISE EXCEPTION 'booking interval contradicts block' USING ERRCODE='23514';
  END IF;
  IF (source.weekday IS NOT NULL AND (extract(isodow FROM local_start)::int<>source.weekday OR EXISTS(SELECT 1 FROM public.schedule_exceptions e WHERE e.clinic_id=NEW.clinic_id AND local_start::date BETWEEN e.from_date AND e.through_date))) OR
     (source.exception_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM public.schedule_exceptions e WHERE e.id=source.exception_id AND local_start::date BETWEEN e.from_date AND e.through_date)) THEN
   RAISE EXCEPTION 'booking date contradicts block exception' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $_$;

CREATE TABLE public.clinical_revisions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    entity text NOT NULL CHECK (entity IN ('PATIENT','ENCOUNTER','PRESCRIPTION','STUDY','VACCINATION')),
    entity_id bigint NOT NULL,
    patient_id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    specialty_context_id bigint NOT NULL,
    owner_user_id character varying(100) NOT NULL,
    actor_user_id character varying(100) NOT NULL,
    occurred_at timestamp with time zone NOT NULL DEFAULT clock_timestamp(),
    operation text NOT NULL CHECK (operation IN ('CORRECT','ANNUL')),
    previous_version bigint NOT NULL CHECK (previous_version >= 0),
    new_version bigint NOT NULL CHECK (new_version = previous_version + 1),
    reason text CHECK (reason IS NULL OR char_length(btrim(reason)) BETWEEN 1 AND 1000),
    previous_content jsonb NOT NULL CHECK (jsonb_typeof(previous_content) = 'object'),
    CHECK (operation <> 'ANNUL' OR reason IS NOT NULL),
    CHECK (entity <> 'PATIENT' OR entity_id = patient_id),
    UNIQUE (entity, entity_id, new_version)
);

CREATE TABLE public.wizard_approvals (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    specialty_id bigint NOT NULL,
    revision bigint NOT NULL CHECK (revision >= 0),
    content jsonb NOT NULL,
    fingerprint text NOT NULL CHECK (fingerprint = encode(sha256(convert_to(content::text,'UTF8')),'hex')),
    actor text NOT NULL CHECK (btrim(actor) <> ''),
    kind text NOT NULL CHECK (kind IN ('CLINICAL','SYNTHETIC')),
    reference text NOT NULL CHECK (char_length(btrim(reference)) BETWEEN 1 AND 500),
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (specialty_id, revision),
    UNIQUE (specialty_id, id)
);

CREATE TABLE public.wizard_approval_revocations (
    approval_id bigint PRIMARY KEY,
    actor text NOT NULL CHECK (btrim(actor) <> ''),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 500),
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE public.clinical_access_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    actor_user_id varchar(100) NOT NULL CHECK (actor_user_id <> ''),
    actor_kind text NOT NULL CHECK (actor_kind IN ('DOCTOR','CATALOG_ADMIN')),
    operation text NOT NULL CHECK (operation ~ '^[A-Z_]{3,50}$'),
    entity_id bigint CHECK (entity_id > 0),
    patient_id bigint CHECK (patient_id > 0),
    record_version bigint CHECK (record_version >= 0),
    owner_user_id varchar(100),
    specialty_context_id bigint CHECK (specialty_context_id > 0),
    outcome text NOT NULL CHECK (outcome IN ('AUTHORIZED','ARTIFACT_READY','REJECTED','FAILED')),
    request_id varchar(32) NOT NULL UNIQUE CHECK (request_id ~ '^[a-f0-9]{32}$'),
    CHECK ((owner_user_id IS NULL) = (specialty_context_id IS NULL)),
    CHECK (owner_user_id IS NULL OR owner_user_id = actor_user_id),
    CHECK (outcome NOT IN ('AUTHORIZED','ARTIFACT_READY') OR owner_user_id IS NOT NULL)
);

CREATE TRIGGER bookings_context_readiness BEFORE INSERT OR UPDATE ON public.bookings FOR EACH ROW EXECUTE FUNCTION public.guard_booking_context_readiness();

CREATE TRIGGER bookings_validate_attendee BEFORE UPDATE ON public.bookings FOR EACH ROW EXECUTE FUNCTION public.validate_booking_attendee();

CREATE TRIGGER bookings_validate_owner_transition BEFORE INSERT OR UPDATE ON public.bookings FOR EACH ROW EXECUTE FUNCTION public.validate_booking_owner_transition();

CREATE TRIGGER calculations_patient_guard BEFORE UPDATE OF patient_id ON public.patient_calculation_logs FOR EACH ROW EXECUTE FUNCTION public.guard_clinical_patient_binding();

CREATE TRIGGER encounters_patient_guard BEFORE UPDATE OF patient_id ON public.clinical_encounters FOR EACH ROW EXECUTE FUNCTION public.guard_clinical_patient_binding();

CREATE TRIGGER encounters_specialty_guard BEFORE INSERT OR UPDATE OF patient_id, specialty_id ON public.clinical_encounters FOR EACH ROW EXECUTE FUNCTION public.guard_encounter_specialty();

CREATE TRIGGER patients_context_guard BEFORE INSERT OR UPDATE OF specialty_context_id, workspace_id, user_id ON public.patients FOR EACH ROW EXECUTE FUNCTION public.guard_patient_context();

CREATE TRIGGER prescriptions_patient_guard BEFORE UPDATE OF patient_id ON public.patient_prescription_records FOR EACH ROW EXECUTE FUNCTION public.guard_clinical_patient_binding();

CREATE TRIGGER specialty_context_binding_guard BEFORE UPDATE OF workspace_id, specialty_id ON public.doctor_specialty_contexts FOR EACH ROW EXECUTE FUNCTION public.guard_context_binding();

CREATE TRIGGER specialty_context_preferences_init AFTER INSERT ON public.doctor_specialty_contexts FOR EACH ROW EXECUTE FUNCTION public.initialize_context_preferences();

CREATE TRIGGER studies_patient_guard BEFORE UPDATE OF patient_id ON public.patient_medical_studies FOR EACH ROW EXECUTE FUNCTION public.guard_clinical_patient_binding();

CREATE TRIGGER vaccinations_patient_guard BEFORE UPDATE OF patient_id ON public.patient_vaccinations FOR EACH ROW EXECUTE FUNCTION public.guard_clinical_patient_binding();

ALTER TABLE ONLY public.booking_confirmation_access
    ADD CONSTRAINT booking_confirmation_access_booking_id_fkey FOREIGN KEY (booking_id) REFERENCES public.bookings(id);

ALTER TABLE ONLY public.booking_events
    ADD CONSTRAINT booking_events_booking_id_fkey FOREIGN KEY (booking_id) REFERENCES public.bookings(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.booking_idempotency
    ADD CONSTRAINT booking_idempotency_booking_id_fkey FOREIGN KEY (booking_id) REFERENCES public.bookings(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.booking_idempotency
    ADD CONSTRAINT booking_idempotency_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.doctor_workspaces(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.booking_public_details
    ADD CONSTRAINT booking_public_details_booking_id_fkey FOREIGN KEY (booking_id) REFERENCES public.bookings(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.booking_portal_codes
    ADD CONSTRAINT booking_portal_codes_booking_id_fkey FOREIGN KEY (booking_id) REFERENCES public.bookings(id);

ALTER TABLE ONLY public.booking_publications
    ADD CONSTRAINT booking_publications_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.doctor_workspaces(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.bookings
    ADD CONSTRAINT bookings_clinic_owner_fk FOREIGN KEY (clinic_id, workspace_id) REFERENCES public.clinics(id, workspace_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.bookings
    ADD CONSTRAINT bookings_context_owner_fkey FOREIGN KEY (specialty_context_id, workspace_id) REFERENCES public.doctor_specialty_contexts(id, workspace_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.bookings
    ADD CONSTRAINT bookings_patient_context_fkey FOREIGN KEY (patient_id, workspace_id, specialty_context_id) REFERENCES public.patients(id, workspace_id, specialty_context_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.bookings
    ADD CONSTRAINT bookings_patient_id_fkey FOREIGN KEY (patient_id) REFERENCES public.patients(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.bookings
    ADD CONSTRAINT bookings_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.doctor_workspaces(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.patient_calculation_logs
    ADD CONSTRAINT calculations_encounter_patient_fkey FOREIGN KEY (encounter_id, patient_id) REFERENCES public.clinical_encounters(id, patient_id);

ALTER TABLE ONLY public.patient_calculation_logs
    ADD CONSTRAINT calculations_patient_owner_fkey FOREIGN KEY (patient_id, user_id) REFERENCES public.patients(id, user_id);

ALTER TABLE ONLY public.clinic_publications
    ADD CONSTRAINT clinic_publications_owner_fk FOREIGN KEY (clinic_id, workspace_id, modality) REFERENCES public.clinics(id, workspace_id, modality) ON DELETE RESTRICT;

ALTER TABLE ONLY public.clinic_publications
    ADD CONSTRAINT clinic_publications_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.booking_publications(workspace_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.clinic_schedules
    ADD CONSTRAINT clinic_schedules_clinic_id_fkey FOREIGN KEY (clinic_id) REFERENCES public.clinics(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.clinic_schedules
    ADD CONSTRAINT clinic_schedules_owner_fkey FOREIGN KEY (clinic_id, workspace_id) REFERENCES public.clinics(id, workspace_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.clinics
    ADD CONSTRAINT clinics_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.doctor_workspaces(id);

ALTER TABLE ONLY public.clinic_specialty_contexts
    ADD CONSTRAINT clinic_specialty_contexts_clinic_fk FOREIGN KEY (clinic_id, workspace_id, modality) REFERENCES public.clinics(id, workspace_id, modality),
    ADD CONSTRAINT clinic_specialty_contexts_context_fk FOREIGN KEY (specialty_context_id, workspace_id) REFERENCES public.doctor_specialty_contexts(id, workspace_id);

ALTER TABLE ONLY public.doctor_specialty_contexts
    ADD CONSTRAINT doctor_specialty_contexts_specialty_id_fkey FOREIGN KEY (specialty_id) REFERENCES public.specialties(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.doctor_specialty_contexts
    ADD CONSTRAINT doctor_specialty_contexts_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.doctor_workspaces(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.doctor_calculator_preferences
    ADD CONSTRAINT fk2gj826se9s45l1hfrlcayck4m FOREIGN KEY (workspace_id) REFERENCES public.doctor_workspaces(id);

ALTER TABLE ONLY public.clinical_encounters
    ADD CONSTRAINT fk4oy7xo3xd5fvlx7niwa2j1md8 FOREIGN KEY (specialty_id) REFERENCES public.specialties(id);

ALTER TABLE ONLY public.prescription_medications
    ADD CONSTRAINT fk6bf0tnv99f40xeerbbr9e6ibr FOREIGN KEY (record_id) REFERENCES public.patient_prescription_records(id);

ALTER TABLE ONLY public.patient_medical_studies
    ADD CONSTRAINT fk6s7aqyrh6om72h8mhw8fvcs9f FOREIGN KEY (patient_id) REFERENCES public.patients(id);

ALTER TABLE ONLY public.doctor_calculator_selections
    ADD CONSTRAINT fk8hfmvjelw72y8sg54b8xj1y0d FOREIGN KEY (preference_id) REFERENCES public.doctor_calculator_preferences(id);

ALTER TABLE ONLY public.doctor_calculator_selections
    ADD CONSTRAINT fkbvvbra0i11ek345erkrm9milk FOREIGN KEY (calculator_id) REFERENCES public.clinical_calculation_definitions(id);

ALTER TABLE ONLY public.clinical_encounters
    ADD CONSTRAINT fkdnca8yvxwdigfmd49hdvmpk2s FOREIGN KEY (patient_id) REFERENCES public.patients(id);

ALTER TABLE ONLY public.patient_calculation_logs
    ADD CONSTRAINT fkf98s121jgxh4lmhicpanghjtr FOREIGN KEY (encounter_id) REFERENCES public.clinical_encounters(id);

ALTER TABLE ONLY public.doctor_calculator_preferences
    ADD CONSTRAINT fkg5sg4spy56luo4cq497ot5f5h FOREIGN KEY (specialty_id) REFERENCES public.specialties(id);

ALTER TABLE ONLY public.wizard_defaults
    ADD CONSTRAINT fkhqth6mq6jqonrt137nuw2upp2 FOREIGN KEY (specialty_id) REFERENCES public.specialties(id);

ALTER TABLE ONLY public.vaccination_doses
    ADD CONSTRAINT fkmb6aujcb9l900ca96c7dq4oef FOREIGN KEY (calendar_id) REFERENCES public.vaccination_calendars(id);

ALTER TABLE ONLY public.specialty_calculator_defaults
    ADD CONSTRAINT fko2360fxxs03ndgt1hdfrhvecj FOREIGN KEY (calculator_id) REFERENCES public.clinical_calculation_definitions(id);

ALTER TABLE ONLY public.patient_vaccinations
    ADD CONSTRAINT fkonf4l7uij7aws0j2a760da6p4 FOREIGN KEY (patient_id) REFERENCES public.patients(id);

ALTER TABLE ONLY public.prescription_diagnostic_orders
    ADD CONSTRAINT fkox8sshlp1ta3n5wg5y4xb3rvd FOREIGN KEY (record_id) REFERENCES public.patient_prescription_records(id);

ALTER TABLE ONLY public.patient_calculation_logs
    ADD CONSTRAINT fkq2orfs0lq9wl7e2pogoro4x99 FOREIGN KEY (patient_id) REFERENCES public.patients(id);

ALTER TABLE ONLY public.patient_prescription_records
    ADD CONSTRAINT fkqjq6qpn5gu8wpmkmaqeg41kfk FOREIGN KEY (patient_id) REFERENCES public.patients(id);

ALTER TABLE ONLY public.specialty_calculator_defaults
    ADD CONSTRAINT fkrchjlfun1ldwe2jr8oshs3ab3 FOREIGN KEY (specialty_id) REFERENCES public.specialties(id);

ALTER TABLE ONLY public.patient_vaccinations
    ADD CONSTRAINT fksm4dyj4jdmxlomqx6u8fkbwfc FOREIGN KEY (dose_id) REFERENCES public.vaccination_doses(id);

ALTER TABLE ONLY public.notification_outbox
    ADD CONSTRAINT notification_outbox_access_id_fkey FOREIGN KEY (access_id) REFERENCES public.staff_access(id);

ALTER TABLE ONLY public.notification_outbox
    ADD CONSTRAINT notification_outbox_booking_id_fkey FOREIGN KEY (booking_id) REFERENCES public.bookings(id);

ALTER TABLE ONLY public.notification_outbox
    ADD CONSTRAINT notification_outbox_recovery_id_fkey FOREIGN KEY (recovery_id) REFERENCES public.staff_email_recovery(id);

ALTER TABLE ONLY public.patient_medical_studies
    ADD CONSTRAINT patient_medical_studies_media_asset_id_fkey FOREIGN KEY (media_asset_id) REFERENCES public.media_assets(asset_id);

ALTER TABLE ONLY public.patients
    ADD CONSTRAINT patients_context_workspace_fkey FOREIGN KEY (specialty_context_id, workspace_id) REFERENCES public.doctor_specialty_contexts(id, workspace_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.patients
    ADD CONSTRAINT patients_workspace_owner_fkey FOREIGN KEY (workspace_id, user_id) REFERENCES public.doctor_workspaces(id, user_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.patient_prescription_records
    ADD CONSTRAINT prescriptions_patient_owner_fkey FOREIGN KEY (patient_id, author_user_id) REFERENCES public.patients(id, user_id);

ALTER TABLE ONLY public.publication_specialty_contexts
    ADD CONSTRAINT publication_specialty_contexts_owner_fkey FOREIGN KEY (specialty_context_id, workspace_id) REFERENCES public.doctor_specialty_contexts(id, workspace_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.publication_specialty_contexts
    ADD CONSTRAINT publication_specialty_contexts_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.booking_publications(workspace_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.schedule_blocks
    ADD CONSTRAINT schedule_blocks_context_fkey FOREIGN KEY (specialty_context_id, workspace_id) REFERENCES public.doctor_specialty_contexts(id, workspace_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.schedule_blocks
    ADD CONSTRAINT schedule_blocks_exception_fkey FOREIGN KEY (exception_id, clinic_id, workspace_id) REFERENCES public.schedule_exceptions(id, clinic_id, workspace_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.schedule_blocks
    ADD CONSTRAINT schedule_blocks_schedule_fkey FOREIGN KEY (clinic_id, workspace_id) REFERENCES public.clinic_schedules(clinic_id, workspace_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.schedule_exceptions
    ADD CONSTRAINT schedule_exceptions_schedule_fkey FOREIGN KEY (clinic_id, workspace_id) REFERENCES public.clinic_schedules(clinic_id, workspace_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.specialty_context_preferences
    ADD CONSTRAINT specialty_context_preferences_specialty_context_id_fkey FOREIGN KEY (specialty_context_id) REFERENCES public.doctor_specialty_contexts(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.staff_access
    ADD CONSTRAINT staff_access_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.staff_accounts(id);

ALTER TABLE ONLY public.staff_access_clinics
    ADD CONSTRAINT staff_access_clinics_access_id_workspace_id_fkey FOREIGN KEY (access_id, workspace_id) REFERENCES public.staff_access(id, workspace_id);

ALTER TABLE ONLY public.staff_access_clinics
    ADD CONSTRAINT staff_access_clinics_clinic_id_workspace_id_fkey FOREIGN KEY (clinic_id, workspace_id) REFERENCES public.clinics(id, workspace_id);

ALTER TABLE ONLY public.staff_access
    ADD CONSTRAINT staff_access_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.doctor_workspaces(id);

ALTER TABLE ONLY public.staff_email_recovery
    ADD CONSTRAINT staff_email_recovery_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.staff_accounts(id);

ALTER TABLE ONLY public.staff_invitations
    ADD CONSTRAINT staff_invitations_access_id_fkey FOREIGN KEY (access_id) REFERENCES public.staff_access(id);

ALTER TABLE ONLY public.staff_security_events
    ADD CONSTRAINT staff_security_events_access_id_fkey FOREIGN KEY (access_id) REFERENCES public.staff_access(id);

ALTER TABLE ONLY public.staff_security_events
    ADD CONSTRAINT staff_security_events_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.staff_accounts(id);

ALTER TABLE ONLY public.staff_sessions
    ADD CONSTRAINT staff_sessions_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.staff_accounts(id);

ALTER TABLE public.clinical_revisions ADD FOREIGN KEY (patient_id,workspace_id,specialty_context_id)
    REFERENCES public.patients(id,workspace_id,specialty_context_id);

CREATE INDEX clinical_revisions_patient_id_idx ON public.clinical_revisions (patient_id,id DESC);
CREATE INDEX patients_annulled_context_idx ON public.patients (specialty_context_id,id DESC) WHERE annulled_at IS NOT NULL;

ALTER TABLE public.clinical_revisions ENABLE ROW LEVEL SECURITY;
CREATE POLICY revision_scope ON public.clinical_revisions FOR SELECT TO doctorchart_app
    USING (owner_user_id = nullif(current_setting('doctorchart.owner', true), '')
        AND specialty_context_id = nullif(current_setting('doctorchart.context', true), '')::bigint);

-- Application writes require an attributed revision even when a new write path omits it.
CREATE FUNCTION public.record_clinical_revision() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path TO 'pg_catalog'
AS $$
DECLARE
    actor text := nullif(current_setting('doctorchart.revision_actor', true), '');
    reason text := nullif(current_setting('doctorchart.revision_reason', true), '');
    scoped boolean := session_user = 'doctorchart_app' OR current_setting('role') = 'doctorchart_app';
    previous jsonb := to_jsonb(OLD);
    patient public.patients%ROWTYPE;
    kind text;
BEGIN
    -- A database owner can still perform privileged maintenance.
    IF actor IS NULL AND NOT scoped THEN
        RETURN NEW;
    END IF;
    IF TG_TABLE_NAME = 'patients' THEN
        kind := 'PATIENT';
        patient := OLD;
    ELSE
        SELECT * INTO STRICT patient FROM public.patients WHERE id = OLD.patient_id;
        kind := CASE TG_TABLE_NAME
            WHEN 'clinical_encounters' THEN 'ENCOUNTER'
            WHEN 'patient_prescription_records' THEN 'PRESCRIPTION'
            WHEN 'patient_medical_studies' THEN 'STUDY'
            WHEN 'patient_vaccinations' THEN 'VACCINATION'
        END;
        IF NEW.patient_id IS DISTINCT FROM OLD.patient_id THEN
            RAISE EXCEPTION 'Invalid clinical revision' USING ERRCODE = '42501';
        END IF;
    END IF;
    IF actor IS DISTINCT FROM patient.user_id OR NEW.version <> OLD.version + 1
       OR NEW.id IS DISTINCT FROM OLD.id OR OLD.annulled_at IS NOT NULL
       OR (kind <> 'PATIENT' AND patient.annulled_at IS NOT NULL)
       OR (scoped AND (
           actor IS DISTINCT FROM nullif(current_setting('doctorchart.owner', true), '')
           OR patient.specialty_context_id IS DISTINCT FROM nullif(current_setting('doctorchart.context', true), '')::bigint
       )) THEN
        RAISE EXCEPTION 'Invalid clinical revision' USING ERRCODE = '42501';
    END IF;
    IF kind = 'PRESCRIPTION' THEN
        previous := previous || jsonb_build_object(
            'medications', coalesce((SELECT jsonb_agg(to_jsonb(m) ORDER BY m.item_position)
                                    FROM public.prescription_medications m WHERE m.record_id = OLD.id), '[]'::jsonb),
            'orders', coalesce((SELECT jsonb_agg(to_jsonb(o) ORDER BY o.item_position)
                               FROM public.prescription_diagnostic_orders o WHERE o.record_id = OLD.id), '[]'::jsonb));
    END IF;
    INSERT INTO public.clinical_revisions(entity,entity_id,patient_id,workspace_id,specialty_context_id,
        owner_user_id,actor_user_id,operation,previous_version,new_version,reason,previous_content)
    VALUES (kind,OLD.id,patient.id,patient.workspace_id,patient.specialty_context_id,patient.user_id,actor,
        CASE WHEN NEW.annulled_at IS NULL THEN 'CORRECT' ELSE 'ANNUL' END,OLD.version,NEW.version,reason,previous);
    RETURN NEW;
END
$$;

CREATE TRIGGER record_clinical_revision AFTER UPDATE ON public.patients
    FOR EACH ROW EXECUTE FUNCTION public.record_clinical_revision();
CREATE TRIGGER record_clinical_revision AFTER UPDATE ON public.clinical_encounters
    FOR EACH ROW EXECUTE FUNCTION public.record_clinical_revision();
CREATE TRIGGER record_clinical_revision AFTER UPDATE ON public.patient_prescription_records
    FOR EACH ROW EXECUTE FUNCTION public.record_clinical_revision();
CREATE TRIGGER record_clinical_revision AFTER UPDATE ON public.patient_medical_studies
    FOR EACH ROW EXECUTE FUNCTION public.record_clinical_revision();
CREATE TRIGGER record_clinical_revision AFTER UPDATE ON public.patient_vaccinations
    FOR EACH ROW EXECUTE FUNCTION public.record_clinical_revision();

-- Request identity is set by Go only for the lifetime of its transaction.
ALTER TABLE public.wizard_approvals ADD FOREIGN KEY (specialty_id) REFERENCES public.specialties(id);
ALTER TABLE public.wizard_approval_revocations ADD FOREIGN KEY (approval_id) REFERENCES public.wizard_approvals(id);

ALTER TABLE public.wizard_defaults ADD CONSTRAINT wizard_defaults_published_approval_fk
    FOREIGN KEY (specialty_id,published_approval_id) REFERENCES public.wizard_approvals(specialty_id,id);

CREATE FUNCTION public.template_review_content(definition jsonb, experience jsonb)
RETURNS jsonb LANGUAGE sql IMMUTABLE STRICT SET search_path TO 'pg_catalog'
AS $$ SELECT jsonb_build_object('definition',definition,'experience',experience) $$;

CREATE FUNCTION public.template_experience(navigation jsonb,tabs jsonb,actions jsonb)
RETURNS jsonb LANGUAGE sql IMMUTABLE STRICT SET search_path TO 'pg_catalog'
AS $$ SELECT jsonb_build_object('navigation',navigation,'patientRecordTabs',tabs,'patientRecordActions',actions) $$;

CREATE FUNCTION public.template_approval_allowed(kind text)
RETURNS boolean LANGUAGE sql STABLE STRICT SET search_path TO 'pg_catalog'
AS $$ SELECT kind='CLINICAL' OR (kind='SYNTHETIC' AND coalesce(current_setting('doctorchart.template_lab',true),'')='on') $$;

-- Used wherever the current publication can create a new context or public offer.
CREATE FUNCTION public.template_available(specialty bigint, require_active boolean DEFAULT true)
RETURNS boolean LANGUAGE sql STABLE SET search_path TO 'pg_catalog'
AS $$
    SELECT EXISTS (
        SELECT 1 FROM public.specialties s JOIN public.wizard_defaults w ON w.specialty_id=s.id
        JOIN public.wizard_approvals a ON a.id=w.published_approval_id AND a.specialty_id=s.id
        LEFT JOIN public.wizard_approval_revocations r ON r.approval_id=a.id
        WHERE s.id=specialty AND (s.active OR NOT require_active) AND w.published_version>0
          AND public.template_approval_allowed(a.kind) AND r.approval_id IS NULL
          AND a.content=public.template_review_content(w.published_definition,
              public.template_experience(s.navigation,s.patient_record_tabs,s.patient_record_actions))
          AND a.fingerprint=encode(sha256(convert_to(public.template_review_content(w.published_definition,
              public.template_experience(s.navigation,s.patient_record_tabs,s.patient_record_actions))::text,'UTF8')),'hex')
    )
$$;

REVOKE EXECUTE ON FUNCTION public.template_review_content(jsonb,jsonb),
    public.template_experience(jsonb,jsonb,jsonb), public.template_approval_allowed(text),
    public.template_available(bigint,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.template_review_content(jsonb,jsonb),
    public.template_experience(jsonb,jsonb,jsonb), public.template_approval_allowed(text),
    public.template_available(bigint,boolean) TO doctorchart_app;
GRANT SELECT, INSERT ON public.wizard_approvals, public.wizard_approval_revocations TO doctorchart_app;

CREATE INDEX clinical_access_events_scope_idx ON public.clinical_access_events (owner_user_id,specialty_context_id,id DESC);
ALTER TABLE public.clinical_access_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY clinical_access_read_scope ON public.clinical_access_events FOR SELECT TO doctorchart_app
    USING (owner_user_id = nullif(current_setting('doctorchart.owner',true),'')
        AND specialty_context_id = nullif(current_setting('doctorchart.context',true),'')::bigint);
CREATE POLICY clinical_access_insert ON public.clinical_access_events FOR INSERT TO doctorchart_app WITH CHECK (true);
GRANT SELECT ON public.clinical_access_events TO doctorchart_app;
GRANT INSERT (actor_user_id,actor_kind,operation,entity_id,patient_id,record_version,
    owner_user_id,specialty_context_id,outcome,request_id) ON public.clinical_access_events TO doctorchart_app;

CREATE TABLE public.clinical_document_issues (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code text NOT NULL UNIQUE CHECK (code ~ '^DC-[a-f0-9]{32}$'),
    owner_user_id varchar(100) NOT NULL,
    specialty_context_id bigint NOT NULL REFERENCES public.doctor_specialty_contexts(id),
    patient_id bigint NOT NULL REFERENCES public.patients(id),
    source text NOT NULL CHECK (source IN ('ENCOUNTER','PRESCRIPTION_RECORD')),
    source_id bigint NOT NULL CHECK (source_id > 0),
    source_version bigint NOT NULL CHECK (source_version >= 0),
    document_type text NOT NULL CHECK (document_type IN ('PRESCRIPTION','STUDY_ORDERS')),
    issuer_specialty text NOT NULL,
    issued_at timestamptz NOT NULL,
    pdf bytea NOT NULL CHECK (octet_length(pdf) BETWEEN 1 AND 4194304),
    sha256 text NOT NULL CHECK (sha256 = encode(pg_catalog.sha256(pdf),'hex'))
);
CREATE INDEX clinical_document_issues_patient_idx
    ON public.clinical_document_issues (owner_user_id,specialty_context_id,patient_id,id DESC);
CREATE TABLE public.clinical_document_annulments (
    document_id bigint PRIMARY KEY REFERENCES public.clinical_document_issues(id),
    owner_user_id varchar(100) NOT NULL,
    specialty_context_id bigint NOT NULL,
    annulled_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 500)
);
ALTER TABLE public.clinical_document_issues ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.clinical_document_annulments ENABLE ROW LEVEL SECURITY;
CREATE POLICY document_issue_scope ON public.clinical_document_issues TO doctorchart_app
    USING (owner_user_id = nullif(current_setting('doctorchart.owner',true),'')
        AND specialty_context_id = nullif(current_setting('doctorchart.context',true),'')::bigint);
CREATE POLICY document_annulment_scope ON public.clinical_document_annulments TO doctorchart_app
    USING (owner_user_id = nullif(current_setting('doctorchart.owner',true),'')
        AND specialty_context_id = nullif(current_setting('doctorchart.context',true),'')::bigint);
GRANT SELECT, INSERT ON public.clinical_document_issues, public.clinical_document_annulments TO doctorchart_app;

CREATE TABLE public.clinical_export_evidence (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_user_id varchar(100) NOT NULL,
    specialty_context_id bigint NOT NULL REFERENCES public.doctor_specialty_contexts(id),
    patient_id bigint NOT NULL REFERENCES public.patients(id),
    prepared_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    sha256 text NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    manifest jsonb NOT NULL CHECK (jsonb_typeof(manifest) = 'object')
);
CREATE INDEX clinical_export_evidence_patient_idx ON public.clinical_export_evidence (owner_user_id,specialty_context_id,patient_id,id DESC);
ALTER TABLE public.clinical_export_evidence ENABLE ROW LEVEL SECURITY;
CREATE POLICY clinical_export_evidence_scope ON public.clinical_export_evidence TO doctorchart_app
    USING (owner_user_id = nullif(current_setting('doctorchart.owner',true),'')
        AND specialty_context_id = nullif(current_setting('doctorchart.context',true),'')::bigint);
GRANT SELECT, INSERT ON public.clinical_export_evidence TO doctorchart_app;

ALTER TABLE public.patients ENABLE ROW LEVEL SECURITY;
CREATE POLICY patient_scope ON public.patients TO doctorchart_app
    USING (user_id = nullif(current_setting('doctorchart.owner', true), '')
       AND specialty_context_id = nullif(current_setting('doctorchart.context', true), '')::bigint)
    WITH CHECK (user_id = nullif(current_setting('doctorchart.owner', true), '')
       AND specialty_context_id = nullif(current_setting('doctorchart.context', true), '')::bigint);
ALTER TABLE public.clinical_encounters ENABLE ROW LEVEL SECURITY;
CREATE POLICY patient_scope ON public.clinical_encounters TO doctorchart_app
    USING (EXISTS (SELECT 1 FROM public.patients p WHERE p.id = clinical_encounters.patient_id))
    WITH CHECK (EXISTS (SELECT 1 FROM public.patients p WHERE p.id = clinical_encounters.patient_id AND p.annulled_at IS NULL));

ALTER TABLE public.patient_calculation_logs ENABLE ROW LEVEL SECURITY;
CREATE POLICY patient_scope ON public.patient_calculation_logs TO doctorchart_app
    USING (EXISTS (SELECT 1 FROM public.patients p WHERE p.id = patient_calculation_logs.patient_id))
    WITH CHECK (EXISTS (SELECT 1 FROM public.patients p WHERE p.id = patient_calculation_logs.patient_id AND p.annulled_at IS NULL));

ALTER TABLE public.patient_medical_studies ENABLE ROW LEVEL SECURITY;
CREATE POLICY patient_scope ON public.patient_medical_studies TO doctorchart_app
    USING (EXISTS (SELECT 1 FROM public.patients p WHERE p.id = patient_medical_studies.patient_id))
    WITH CHECK (EXISTS (SELECT 1 FROM public.patients p WHERE p.id = patient_medical_studies.patient_id AND p.annulled_at IS NULL));

-- Recovery verifies reference existence through this single operational column.
CREATE POLICY media_reference ON public.patient_medical_studies FOR SELECT TO doctorchart_media USING (true);
GRANT SELECT (media_asset_id) ON public.patient_medical_studies TO doctorchart_media;

ALTER TABLE public.patient_prescription_records ENABLE ROW LEVEL SECURITY;
CREATE POLICY patient_scope ON public.patient_prescription_records TO doctorchart_app
    USING (EXISTS (SELECT 1 FROM public.patients p WHERE p.id = patient_prescription_records.patient_id))
    WITH CHECK (EXISTS (SELECT 1 FROM public.patients p WHERE p.id = patient_prescription_records.patient_id AND p.annulled_at IS NULL));

ALTER TABLE public.patient_vaccinations ENABLE ROW LEVEL SECURITY;
CREATE POLICY patient_scope ON public.patient_vaccinations TO doctorchart_app
    USING (EXISTS (SELECT 1 FROM public.patients p WHERE p.id = patient_vaccinations.patient_id))
    WITH CHECK (EXISTS (SELECT 1 FROM public.patients p WHERE p.id = patient_vaccinations.patient_id AND p.annulled_at IS NULL));

ALTER TABLE public.prescription_diagnostic_orders ENABLE ROW LEVEL SECURITY;
CREATE POLICY patient_scope ON public.prescription_diagnostic_orders TO doctorchart_app
    USING (EXISTS (SELECT 1 FROM public.patient_prescription_records r WHERE r.id = prescription_diagnostic_orders.record_id))
    WITH CHECK (EXISTS (SELECT 1 FROM public.patient_prescription_records r WHERE r.id = prescription_diagnostic_orders.record_id));

ALTER TABLE public.prescription_medications ENABLE ROW LEVEL SECURITY;
CREATE POLICY patient_scope ON public.prescription_medications TO doctorchart_app
    USING (EXISTS (SELECT 1 FROM public.patient_prescription_records r WHERE r.id = prescription_medications.record_id))
    WITH CHECK (EXISTS (SELECT 1 FROM public.patient_prescription_records r WHERE r.id = prescription_medications.record_id));

-- Whole-account quotas never return clinical rows.
CREATE FUNCTION public.patient_quota_count() RETURNS bigint
LANGUAGE sql STABLE SECURITY DEFINER SET search_path TO 'pg_catalog'
AS $$
    SELECT count(*) FROM public.patients
    WHERE user_id = nullif(current_setting('doctorchart.owner', true), '') AND annulled_at IS NULL
      AND EXISTS (
          SELECT 1 FROM public.doctor_specialty_contexts c
          JOIN public.doctor_workspaces w ON w.id = c.workspace_id
          WHERE c.id = nullif(current_setting('doctorchart.context', true), '')::bigint
            AND w.user_id = nullif(current_setting('doctorchart.owner', true), '')
      )
$$;

-- Check all study references before retiring a file from the durable queue.
CREATE FUNCTION public.study_file_referenced(key text) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path TO 'pg_catalog'
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM public.study_file_deletions d WHERE d.storage_key = key) THEN
        RAISE EXCEPTION 'Study deletion intent is missing' USING ERRCODE = '42501';
    END IF;
    RETURN EXISTS (SELECT 1 FROM public.patient_medical_studies s WHERE s.stored_file_name = key);
END
$$;

-- Associate a newly prepared private asset in the same transaction as its study.
CREATE FUNCTION public.record_study_asset(patient bigint, asset uuid, document jsonb, event_id uuid, event jsonb)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path TO 'pg_catalog'
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM public.patients p
        WHERE p.id = patient
          AND p.annulled_at IS NULL
          AND p.user_id = nullif(current_setting('doctorchart.owner', true), '')
          AND p.specialty_context_id = nullif(current_setting('doctorchart.context', true), '')::bigint
          AND p.user_id = document->>'owner_id'
          AND document->'associations' = jsonb_build_array(
              jsonb_build_object('entity_type', 'patient', 'entity_id', p.media_identity::text, 'role', 'study'))
    ) OR document->>'status' IS DISTINCT FROM 'associated'
      OR document->>'visibility' IS DISTINCT FROM 'private'
      OR event->'asset' IS DISTINCT FROM document
      OR event->>'event_id' IS DISTINCT FROM event_id::text
      OR event->>'kind' IS DISTINCT FROM 'association-confirmed' THEN
        RAISE EXCEPTION 'Invalid study asset association' USING ERRCODE = '42501';
    END IF;
    INSERT INTO public.media_assets(asset_id, document) VALUES(asset, document);
    INSERT INTO public.media_events(event_id, document) VALUES(event_id, event);
END
$$;

REVOKE EXECUTE ON FUNCTION public.patient_quota_count(), public.study_file_referenced(text),
    public.record_study_asset(bigint, uuid, jsonb, uuid, jsonb),
    public.guard_context_binding(), public.validate_booking_owner_transition(), public.record_clinical_revision() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.patient_quota_count(), public.study_file_referenced(text),
    public.record_study_asset(bigint, uuid, jsonb, uuid, jsonb) TO doctorchart_app;

GRANT USAGE ON SCHEMA public TO doctorchart_app, doctorchart_media;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO doctorchart_app;
GRANT SELECT ON public.app_schema_state, public.chart_definitions, public.clinical_calculation_definitions,
    public.specialty_calculator_defaults, public.vaccination_calendars, public.vaccination_doses TO doctorchart_app;
GRANT SELECT, INSERT, UPDATE ON public.booking_confirmation_access, public.booking_portal_codes, public.booking_publications, public.bookings,
    public.clinic_publications, public.clinic_schedules, public.clinics, public.doctor_calculator_preferences,
    public.doctor_chart_configs, public.doctor_specialty_contexts, public.doctor_workspaces,
    public.notification_outbox, public.specialties, public.specialty_context_preferences,
    public.staff_access, public.staff_accounts, public.staff_email_recovery, public.staff_invitations,
    public.staff_sessions, public.wizard_defaults TO doctorchart_app;
GRANT SELECT, UPDATE ON public.clinic_entitlement_epoch TO doctorchart_app;
GRANT SELECT, INSERT ON public.booking_events, public.booking_idempotency, public.booking_public_details,
    public.patient_calculation_logs, public.staff_security_events TO doctorchart_app;
GRANT SELECT, INSERT ON public.prescription_diagnostic_orders, public.prescription_medications TO doctorchart_app;
GRANT SELECT ON public.clinical_revisions TO doctorchart_app;
GRANT SELECT, INSERT, DELETE ON public.doctor_calculator_selections,
    public.clinic_specialty_contexts, public.publication_specialty_contexts, public.staff_access_clinics TO doctorchart_app;
GRANT SELECT, INSERT, UPDATE ON public.clinical_encounters, public.patient_vaccinations, public.patients TO doctorchart_app;
GRANT SELECT, INSERT ON public.patient_medical_studies, public.patient_prescription_records TO doctorchart_app;
GRANT UPDATE (annulled_at,version) ON public.patient_medical_studies, public.patient_prescription_records TO doctorchart_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON public.public_booking_rate_windows, public.schedule_blocks, public.schedule_exceptions,
    public.staff_rate_windows, public.study_file_deletions TO doctorchart_app;

-- The shared storage library can read its operational catalog without patient-table grants.
GRANT SELECT, INSERT, UPDATE ON public.media_assets TO doctorchart_media;
GRANT SELECT, INSERT, DELETE ON public.media_events TO doctorchart_media;
-- SELECT FOR UPDATE SKIP LOCKED requires UPDATE on at least one column.
-- No UPDATE grant on the event document is needed by the media library.
GRANT UPDATE (event_id) ON public.media_events TO doctorchart_media;
GRANT SELECT ON public.app_schema_state TO doctorchart_media;
