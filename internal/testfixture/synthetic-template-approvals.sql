-- Disposable test fixtures only. The normal binary refuses synthetic approvals.
WITH publication AS (
    SELECT s.id, w.version AS revision,
        public.template_review_content(w.published_definition,
            public.template_experience(s.navigation,s.patient_record_tabs,s.patient_record_actions)) AS content
    FROM specialties s JOIN wizard_defaults w ON w.specialty_id=s.id
    WHERE s.code IN ('pediatrics','general_medicine','dermatology')
        AND w.published_approval_id IS NULL AND w.published_version=1
), approved AS (
    INSERT INTO wizard_approvals(specialty_id,revision,content,fingerprint,actor,kind,reference)
    SELECT id,revision,content,encode(sha256(convert_to(content::text,'UTF8')),'hex'),
        '22222222-2222-4222-8222-222222222222','SYNTHETIC','Publicación inicial del laboratorio M6'
    FROM publication RETURNING specialty_id,id
)
UPDATE wizard_defaults w SET published_approval_id=approved.id,version=w.version+1
FROM approved WHERE w.specialty_id=approved.specialty_id;
