-- Keep the administrative workspace and all clinical identities. Clinical UI
-- preferences acquire their own context/version without bumping wizard versions.
CREATE TABLE specialty_context_preferences (
 specialty_context_id bigint PRIMARY KEY REFERENCES doctor_specialty_contexts(id) ON DELETE RESTRICT,
 version bigint NOT NULL DEFAULT 0 CHECK(version>=0),
 patient_columns_config jsonb NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(patient_columns_config)='object'),
 quick_notes text
);
INSERT INTO specialty_context_preferences(specialty_context_id,version,patient_columns_config,quick_notes)
 SELECT c.id,w.version,COALESCE(w.patient_columns_config,'{}'),w.quick_notes FROM doctor_workspaces w JOIN doctor_specialty_contexts c ON c.workspace_id=w.id;
-- Preserve the historical workspace values for rollback/export; M5 reads and writes only the context preferences.
CREATE FUNCTION public.initialize_context_preferences() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN INSERT INTO public.specialty_context_preferences(specialty_context_id) VALUES(NEW.id); RETURN NEW; END $$;
CREATE TRIGGER specialty_context_preferences_init AFTER INSERT ON doctor_specialty_contexts FOR EACH ROW EXECUTE FUNCTION public.initialize_context_preferences();

CREATE TABLE publication_specialty_contexts (
 workspace_id bigint NOT NULL REFERENCES booking_publications(workspace_id) ON DELETE RESTRICT,
 specialty_context_id bigint NOT NULL,
 PRIMARY KEY(workspace_id,specialty_context_id),
 CONSTRAINT publication_specialty_contexts_owner_fkey FOREIGN KEY(specialty_context_id,workspace_id) REFERENCES doctor_specialty_contexts(id,workspace_id) ON DELETE RESTRICT
);
INSERT INTO publication_specialty_contexts(workspace_id,specialty_context_id)
 SELECT p.workspace_id,c.id FROM booking_publications p JOIN doctor_specialty_contexts c ON c.workspace_id=p.workspace_id WHERE c.onboarding_completed AND c.definition IS NOT NULL;

-- Reject new service operations in an unfinished context. Existing reservations
-- retain cancellation, status, linking and immutable historical classification.
CREATE FUNCTION public.guard_booking_context_readiness() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='INSERT' OR (NEW.start_at,NEW.end_at,NEW.clinic_id,NEW.provenance) IS DISTINCT FROM (OLD.start_at,OLD.end_at,OLD.clinic_id,OLD.provenance) THEN
  PERFORM id FROM public.doctor_specialty_contexts WHERE id=NEW.specialty_context_id AND workspace_id=NEW.workspace_id AND onboarding_completed AND definition IS NOT NULL FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'booking context is not ready' USING ERRCODE='23514'; END IF;
 END IF;RETURN NEW;
END $$;
CREATE TRIGGER bookings_context_readiness BEFORE INSERT OR UPDATE ON bookings FOR EACH ROW EXECUTE FUNCTION public.guard_booking_context_readiness();

-- This is the final transition: all readers/writers are updated together.
ALTER TABLE doctor_specialty_contexts DROP CONSTRAINT doctor_specialty_contexts_single_context_key;
