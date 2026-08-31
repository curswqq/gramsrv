ALTER TABLE public.user_business_profiles
    ADD COLUMN IF NOT EXISTS sponsored_messages_enabled boolean DEFAULT false NOT NULL;

ALTER TABLE public.quick_reply_messages
    ADD COLUMN IF NOT EXISTS media jsonb DEFAULT '{}'::jsonb NOT NULL;

ALTER TABLE public.business_connected_bots
    ADD COLUMN IF NOT EXISTS connection_id text;

UPDATE public.business_connected_bots
SET connection_id = md5(random()::text || clock_timestamp()::text || owner_user_id::text)
                 || md5(random()::text || clock_timestamp()::text || bot_user_id::text)
WHERE connection_id IS NULL OR btrim(connection_id) = '';

ALTER TABLE public.business_connected_bots
    ALTER COLUMN connection_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS business_connected_bots_connection_id_uniq
    ON public.business_connected_bots (connection_id);
