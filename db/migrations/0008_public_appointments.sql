CREATE TABLE public.booking_publications (
 workspace_id bigint PRIMARY KEY REFERENCES public.doctor_workspaces(id) ON DELETE RESTRICT,
 public_id uuid NOT NULL UNIQUE,
 name varchar(160) NOT NULL CHECK(length(btrim(name))>0),
 description varchar(1200) NOT NULL DEFAULT '',
 published boolean NOT NULL DEFAULT false,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 questions jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(questions)='array' AND jsonb_array_length(questions)<=8)
);
CREATE TABLE public.clinic_publications (
 clinic_id bigint PRIMARY KEY,
 workspace_id bigint NOT NULL REFERENCES public.booking_publications(workspace_id) ON DELETE RESTRICT,
 public_id uuid NOT NULL UNIQUE,
 enabled boolean NOT NULL DEFAULT false,
 address varchar(255) NOT NULL,
 CONSTRAINT clinic_publications_owner_fk FOREIGN KEY(clinic_id,workspace_id) REFERENCES public.clinics(id,workspace_id) ON DELETE RESTRICT
);
CREATE TABLE public.booking_public_details (
 booking_id bigint PRIMARY KEY REFERENCES public.bookings(id) ON DELETE RESTRICT,
 reference uuid NOT NULL UNIQUE,
 form_version bigint NOT NULL CHECK(form_version>0),
 form_snapshot jsonb NOT NULL CHECK(jsonb_typeof(form_snapshot)='object'),
 answers jsonb NOT NULL CHECK(jsonb_typeof(answers)='object'),
 document_country varchar(2) NOT NULL,
 document_type varchar(12) NOT NULL CHECK(document_type IN ('DNI','PASSPORT')),
 document varchar(24) NOT NULL,
 email varchar(254) NOT NULL,
 challenge_hash varchar(64) NOT NULL UNIQUE CHECK(challenge_hash ~ '^[0-9a-f]{64}$'),
 doctor_name varchar(160) NOT NULL,
 clinic_name varchar(160) NOT NULL,
 address varchar(255) NOT NULL,
 timezone varchar(100) NOT NULL
);
CREATE TABLE public.public_booking_rate_windows (
 subject varchar(64) NOT NULL CHECK(subject ~ '^[0-9a-f]{64}$'),
 kind varchar(16) NOT NULL,
 minute bigint NOT NULL,
 used integer NOT NULL CHECK(used>0),
 PRIMARY KEY(subject,kind,minute)
);
CREATE INDEX public_booking_rate_expiry_idx ON public.public_booking_rate_windows(minute);
