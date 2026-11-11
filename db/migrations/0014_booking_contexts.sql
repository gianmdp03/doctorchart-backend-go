-- Classify legacy reservations by the sole verified workspace context, without
-- inventing a historical block. Events, receipts and business versions stay intact.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM bookings b LEFT JOIN doctor_specialty_contexts c ON c.workspace_id=b.workspace_id
           WHERE c.id IS NULL) OR EXISTS(
  SELECT 1 FROM bookings b JOIN patients p ON p.id=b.patient_id
  JOIN doctor_specialty_contexts c ON c.workspace_id=b.workspace_id
  WHERE p.workspace_id<>b.workspace_id OR p.specialty_context_id<>c.id) THEN
  RAISE EXCEPTION 'M4: booking has no unambiguous owner/context or has an incompatible patient' USING ERRCODE='23514';
 END IF;
END $$;

CREATE FUNCTION public.booking_clinic_snapshot(clinic bigint) RETURNS jsonb
LANGUAGE sql STABLE SET search_path=pg_catalog AS $$
 SELECT jsonb_build_object('name',c.name,'address',c.address,'publicAddress',COALESCE(p.address,''),'timezone',c.timezone)
 FROM public.clinics c LEFT JOIN public.clinic_publications p ON p.clinic_id=c.id AND p.enabled WHERE c.id=clinic
$$;
CREATE FUNCTION public.booking_block_snapshot(block bigint) RETURNS jsonb
LANGUAGE sql STABLE SET search_path=pg_catalog AS $$
 SELECT jsonb_build_object('kind','SCHEDULE_BLOCK','blockId',b.id::text,'scheduleVersion',s.version::text,
  'weekday',COALESCE(b.weekday,0),'exceptionId',COALESCE(b.exception_id::text,''),
  'from',COALESCE(e.from_date::text,''),'through',COALESCE(e.through_date::text,''),
  'startMinute',b.start_minute,'endMinute',b.end_minute,'slotMinutes',b.slot_minutes,'timezone',c.timezone)
 FROM public.schedule_blocks b JOIN public.clinic_schedules s ON s.clinic_id=b.clinic_id
 JOIN public.clinics c ON c.id=b.clinic_id LEFT JOIN public.schedule_exceptions e ON e.id=b.exception_id WHERE b.id=block
$$;

ALTER TABLE bookings ADD COLUMN specialty_context_id bigint,
 ADD COLUMN specialty_snapshot jsonb, ADD COLUMN clinic_snapshot jsonb, ADD COLUMN provenance jsonb;
DROP TRIGGER bookings_validate_owner_transition ON bookings;
UPDATE bookings b SET specialty_context_id=c.id,
 specialty_snapshot=jsonb_build_object('id',s.id::text,'code',s.code,'name',s.display_name),
 clinic_snapshot=public.booking_clinic_snapshot(b.clinic_id),
 provenance=jsonb_build_object('kind','LEGACY_CONTEXT_ONLY')
 FROM doctor_specialty_contexts c JOIN specialties s ON s.id=c.specialty_id WHERE c.workspace_id=b.workspace_id;
ALTER TABLE patients ADD CONSTRAINT patients_id_workspace_context_key UNIQUE(id,workspace_id,specialty_context_id);
ALTER TABLE bookings ALTER COLUMN specialty_context_id SET NOT NULL,
 ALTER COLUMN specialty_snapshot SET NOT NULL, ALTER COLUMN clinic_snapshot SET NOT NULL, ALTER COLUMN provenance SET NOT NULL,
 ADD CONSTRAINT bookings_context_owner_fkey FOREIGN KEY(specialty_context_id,workspace_id) REFERENCES doctor_specialty_contexts(id,workspace_id) ON DELETE RESTRICT,
 ADD CONSTRAINT bookings_patient_context_fkey FOREIGN KEY(patient_id,workspace_id,specialty_context_id) REFERENCES patients(id,workspace_id,specialty_context_id) ON DELETE RESTRICT,
 ADD CONSTRAINT bookings_specialty_snapshot_check CHECK(jsonb_typeof(specialty_snapshot)='object' AND COALESCE(specialty_snapshot->>'id','') ~ '^[1-9][0-9]*$' AND COALESCE(specialty_snapshot->>'code','')<>'' AND COALESCE(specialty_snapshot->>'name','')<>''),
 ADD CONSTRAINT bookings_clinic_snapshot_check CHECK(jsonb_typeof(clinic_snapshot)='object' AND COALESCE(clinic_snapshot->>'name','')<>'' AND COALESCE(clinic_snapshot->>'timezone','')<>''),
 ADD CONSTRAINT bookings_provenance_check CHECK(jsonb_typeof(provenance)='object' AND COALESCE(provenance->>'kind','') IN ('LEGACY_CONTEXT_ONLY','SCHEDULE_BLOCK'));
CREATE INDEX bookings_context_start_idx ON bookings(specialty_context_id,start_at,id);

CREATE OR REPLACE FUNCTION public.validate_booking_owner_transition() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE source public.schedule_blocks%ROWTYPE; zone text; local_start timestamp; minute integer; moving boolean;
BEGIN
 PERFORM id FROM public.doctor_specialty_contexts WHERE id=NEW.specialty_context_id FOR SHARE;
 IF NEW.patient_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM public.patients p WHERE p.id=NEW.patient_id AND p.workspace_id=NEW.workspace_id AND p.specialty_context_id=NEW.specialty_context_id) THEN
  RAISE EXCEPTION 'booking patient context mismatch' USING ERRCODE='23514',CONSTRAINT='bookings_patient_context_fkey';
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
END $$;
CREATE TRIGGER bookings_validate_owner_transition BEFORE INSERT OR UPDATE ON bookings FOR EACH ROW EXECUTE FUNCTION public.validate_booking_owner_transition();

CREATE OR REPLACE FUNCTION public.guard_context_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.workspace_id<>OLD.workspace_id OR (NEW.specialty_id<>OLD.specialty_id AND
  (EXISTS(SELECT 1 FROM public.patients WHERE specialty_context_id=OLD.id) OR EXISTS(SELECT 1 FROM public.bookings WHERE specialty_context_id=OLD.id))) THEN
  RAISE EXCEPTION 'Clinical context binding is immutable once used' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
ALTER TABLE booking_events DROP CONSTRAINT booking_events_kind_check,
 ADD CONSTRAINT booking_events_kind_check CHECK(kind IN ('CREATED','STATUS_CHANGED','RESCHEDULED','PATIENT_LINKED'));
