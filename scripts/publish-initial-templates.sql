-- Run with psql -XqAt -v ON_ERROR_STOP=1 -v apply=false to obtain the
-- manifest. To publish, pass -v apply=true -v expected="$manifest".
BEGIN;
SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '20s';
SELECT pg_advisory_xact_lock(17209, 41001) AS lock_acquired \gset

CREATE TEMP TABLE initial_templates ON COMMIT DROP AS
SELECT s.id, s.code,
       encode(sha256(convert_to(public.template_review_content(w.draft_definition,
           public.template_experience(s.navigation,s.patient_record_tabs,s.patient_record_actions))::text,'UTF8')),'hex') AS fingerprint
FROM public.specialties s
JOIN public.wizard_defaults w ON w.specialty_id=s.id
WHERE w.version=0 AND w.published_approval_id IS NULL AND w.draft_experience IS NULL
  AND s.code IN (
    'allergy_immunology', 'cardiology', 'dermatology', 'endocrinology',
    'gastroenterology', 'general_medicine', 'geriatrics', 'gynecology',
    'internal_medicine', 'neurology', 'obstetrics', 'ophthalmology',
    'otolaryngology', 'pediatrics', 'psychiatry', 'pulmonology',
    'rheumatology', 'traumatology_orthopedics', 'urology'
  );

SELECT count(*)=19 AND count(fingerprint)=19 AS complete,
       coalesce(jsonb_object_agg(code,fingerprint), '{}'::jsonb) AS manifest
FROM initial_templates \gset
\if :complete
\else
  DO $$ BEGIN RAISE EXCEPTION 'The 19 unmodified seed drafts are required'; END $$;
\endif

\if :apply
  SELECT :'manifest'::jsonb = :'expected'::jsonb AS matches \gset
  \if :matches
    UPDATE public.wizard_defaults w
    SET published_definition=w.draft_definition, published_version=w.published_version+1,
        version=w.version+1, updated_at=greatest(clock_timestamp(),w.updated_at+interval '1 microsecond')
    FROM initial_templates t
    WHERE w.specialty_id=t.id;

    UPDATE public.specialties s
    SET active=true, version=s.version+1,
        updated_at=greatest(clock_timestamp(),s.updated_at+interval '1 microsecond')
    FROM initial_templates t
    WHERE s.id=t.id AND NOT s.active;
  \else
    DO $$ BEGIN RAISE EXCEPTION 'The initial template manifest changed'; END $$;
  \endif
\endif
SELECT :'manifest';
COMMIT;
