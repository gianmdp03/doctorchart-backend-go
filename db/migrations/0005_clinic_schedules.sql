-- M2 schema 4 -> M3 schema 5. Add agenda configuration only; no generated slots.
-- Existing clinics and all clinical resources remain untouched.
CREATE TABLE public.clinic_schedules (
 clinic_id bigint PRIMARY KEY REFERENCES public.clinics(id) ON DELETE RESTRICT,
 slot_minutes integer NOT NULL CHECK (slot_minutes BETWEEN 5 AND 480),
 min_notice_minutes integer NOT NULL CHECK (min_notice_minutes BETWEEN 0 AND 525600),
 max_advance_days integer NOT NULL CHECK (max_advance_days BETWEEN 1 AND 366),
 weekly jsonb NOT NULL CHECK (jsonb_typeof(weekly)='array' AND jsonb_array_length(weekly)<=7),
 exceptions jsonb NOT NULL CHECK (jsonb_typeof(exceptions)='array' AND jsonb_array_length(exceptions)<=100),
 version bigint NOT NULL DEFAULT 1 CHECK (version>0)
);
COMMENT ON TABLE public.clinic_schedules IS 'Local civil agenda configuration; availability is calculated, never materialized.';

