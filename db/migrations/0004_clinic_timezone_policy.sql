-- Schema 3 -> 4. Preserve rows; refuse a policy change requiring owner action.
-- Prevent an old writer from inserting an excluded zone during preflight.
LOCK TABLE public.clinics IN SHARE ROW EXCLUSIVE MODE;
DO $$
DECLARE incompatible bigint;
BEGIN
    SELECT count(*) INTO incompatible FROM public.clinics WHERE timezone LIKE 'Etc/%';
    IF incompatible > 0 THEN
        RAISE EXCEPTION 'doctorchart migration 4 blocked: % clinic(s) use excluded Etc/ timezones; explicit owner reconciliation required; no data changed', incompatible;
    END IF;
END
$$;

CREATE OR REPLACE FUNCTION public.is_iana_timezone(value text) RETURNS boolean
LANGUAGE sql STABLE STRICT
SET search_path = pg_catalog
AS $$
    SELECT (value = 'UTC' OR position('/' in value) > 0)
       AND value NOT LIKE 'posix/%' AND value NOT LIKE 'right/%'
       AND value NOT LIKE 'Etc/%'
       AND EXISTS (SELECT 1 FROM pg_catalog.pg_timezone_names WHERE name = value)
$$;

-- PostgreSQL does not recheck existing rows when a CHECK function changes.
-- Recreate and validate the same constraint inside the upgrade transaction.
ALTER TABLE public.clinics DROP CONSTRAINT clinics_timezone_check,
    ADD CONSTRAINT clinics_timezone_check CHECK (public.is_iana_timezone(timezone));
