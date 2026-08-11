CREATE TABLE public.stars_bulk_grants (
    command_key text PRIMARY KEY,
    amount bigint NOT NULL CHECK (amount > 0),
    recipient_count integer DEFAULT 0 NOT NULL CHECK (recipient_count >= 0),
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
