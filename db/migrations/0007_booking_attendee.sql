-- Add administrative attendee identity; historical rows/receipts/events stay intact.
ALTER TABLE public.bookings ADD COLUMN attendee_name varchar(160)
 CONSTRAINT bookings_attendee_name_check CHECK (attendee_name IS NULL OR length(btrim(attendee_name))>0);
CREATE FUNCTION public.validate_booking_attendee() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.attendee_name IS DISTINCT FROM OLD.attendee_name THEN
  RAISE EXCEPTION 'booking attendee is immutable' USING ERRCODE='23514',CONSTRAINT='bookings_attendee_immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER bookings_validate_attendee BEFORE UPDATE ON public.bookings
 FOR EACH ROW EXECUTE FUNCTION public.validate_booking_attendee();
