-- Shared invalidation fence. Does not change clinics or any clinical object.
CREATE TABLE public.clinic_entitlement_epoch (
    id integer PRIMARY KEY CHECK (id = 1),
    generation bigint NOT NULL CHECK (generation >= 0)
);
INSERT INTO public.clinic_entitlement_epoch(id, generation) VALUES (1, 0);
