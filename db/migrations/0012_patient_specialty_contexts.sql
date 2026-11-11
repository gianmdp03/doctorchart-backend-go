-- Refuse ambiguous/inconsistent legacy data; never invent clinical ownership.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM patients p LEFT JOIN doctor_workspaces w ON w.user_id=p.user_id
            LEFT JOIN doctor_specialty_contexts c ON c.workspace_id=w.id WHERE c.id IS NULL) THEN
  RAISE EXCEPTION 'M2: patient owner has no clinical context' USING ERRCODE='23514';
 END IF;
 IF EXISTS (SELECT 1 FROM clinical_encounters e JOIN patients p ON p.id=e.patient_id
            JOIN doctor_workspaces w ON w.user_id=p.user_id JOIN doctor_specialty_contexts c ON c.workspace_id=w.id
            WHERE e.specialty_id<>c.specialty_id) THEN
  RAISE EXCEPTION 'M2: encounter specialty contradicts patient context' USING ERRCODE='23514';
 END IF;
END $$;

ALTER TABLE doctor_workspaces ADD CONSTRAINT doctor_workspaces_id_owner_key UNIQUE(id,user_id);
ALTER TABLE doctor_specialty_contexts ADD CONSTRAINT doctor_specialty_contexts_id_workspace_key UNIQUE(id,workspace_id);
ALTER TABLE patients ADD COLUMN workspace_id bigint, ADD COLUMN specialty_context_id bigint;
UPDATE patients p SET workspace_id=w.id,specialty_context_id=c.id
 FROM doctor_workspaces w JOIN doctor_specialty_contexts c ON c.workspace_id=w.id WHERE p.user_id=w.user_id;
ALTER TABLE patients
 ALTER COLUMN workspace_id SET NOT NULL, ALTER COLUMN specialty_context_id SET NOT NULL,
 ADD CONSTRAINT patients_workspace_owner_fkey FOREIGN KEY(workspace_id,user_id) REFERENCES doctor_workspaces(id,user_id) ON DELETE RESTRICT,
 ADD CONSTRAINT patients_context_workspace_fkey FOREIGN KEY(specialty_context_id,workspace_id) REFERENCES doctor_specialty_contexts(id,workspace_id) ON DELETE RESTRICT,
 ADD CONSTRAINT patients_id_owner_key UNIQUE(id,user_id);
CREATE INDEX patients_context_name_id_idx ON patients(specialty_context_id,full_name,id);
CREATE INDEX patients_context_id_idx ON patients(specialty_context_id,id);

ALTER TABLE clinical_encounters ADD CONSTRAINT clinical_encounters_id_patient_key UNIQUE(id,patient_id);
ALTER TABLE patient_prescription_records ADD CONSTRAINT prescriptions_patient_owner_fkey
 FOREIGN KEY(patient_id,author_user_id) REFERENCES patients(id,user_id);
ALTER TABLE patient_calculation_logs
 ADD CONSTRAINT calculations_patient_owner_fkey FOREIGN KEY(patient_id,user_id) REFERENCES patients(id,user_id),
 ADD CONSTRAINT calculations_encounter_patient_fkey FOREIGN KEY(encounter_id,patient_id) REFERENCES clinical_encounters(id,patient_id);

CREATE FUNCTION public.guard_patient_context() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND (NEW.specialty_context_id,NEW.workspace_id,NEW.user_id) IS DISTINCT FROM (OLD.specialty_context_id,OLD.workspace_id,OLD.user_id) THEN
  RAISE EXCEPTION 'Patient clinical ownership is immutable' USING ERRCODE='23514';
 END IF;
 -- Serialize inserts with specialty edits; FK checks separately enforce owner.
 PERFORM id FROM public.doctor_specialty_contexts WHERE id=NEW.specialty_context_id FOR SHARE;
 RETURN NEW;
END $$;
CREATE TRIGGER patients_context_guard BEFORE INSERT OR UPDATE OF specialty_context_id,workspace_id,user_id ON public.patients
 FOR EACH ROW EXECUTE FUNCTION public.guard_patient_context();

CREATE FUNCTION public.guard_context_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.workspace_id<>OLD.workspace_id OR (NEW.specialty_id<>OLD.specialty_id AND EXISTS(SELECT 1 FROM public.patients WHERE specialty_context_id=OLD.id)) THEN
  RAISE EXCEPTION 'Clinical context binding is immutable once used' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER specialty_context_binding_guard BEFORE UPDATE OF workspace_id,specialty_id ON public.doctor_specialty_contexts
 FOR EACH ROW EXECUTE FUNCTION public.guard_context_binding();

CREATE FUNCTION public.guard_clinical_patient_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.patient_id<>OLD.patient_id THEN
  RAISE EXCEPTION 'Clinical record patient is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER encounters_patient_guard BEFORE UPDATE OF patient_id ON public.clinical_encounters FOR EACH ROW EXECUTE FUNCTION public.guard_clinical_patient_binding();
CREATE TRIGGER prescriptions_patient_guard BEFORE UPDATE OF patient_id ON public.patient_prescription_records FOR EACH ROW EXECUTE FUNCTION public.guard_clinical_patient_binding();
CREATE TRIGGER studies_patient_guard BEFORE UPDATE OF patient_id ON public.patient_medical_studies FOR EACH ROW EXECUTE FUNCTION public.guard_clinical_patient_binding();
CREATE TRIGGER vaccinations_patient_guard BEFORE UPDATE OF patient_id ON public.patient_vaccinations FOR EACH ROW EXECUTE FUNCTION public.guard_clinical_patient_binding();
CREATE TRIGGER calculations_patient_guard BEFORE UPDATE OF patient_id ON public.patient_calculation_logs FOR EACH ROW EXECUTE FUNCTION public.guard_clinical_patient_binding();

CREATE FUNCTION public.guard_encounter_specialty() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM public.patients p JOIN public.doctor_specialty_contexts c ON c.id=p.specialty_context_id
               WHERE p.id=NEW.patient_id AND c.specialty_id=NEW.specialty_id) THEN
  RAISE EXCEPTION 'Encounter specialty contradicts patient context' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER encounters_specialty_guard BEFORE INSERT OR UPDATE OF patient_id,specialty_id ON public.clinical_encounters
 FOR EACH ROW EXECUTE FUNCTION public.guard_encounter_specialty();
