DROP INDEX IF EXISTS public.business_connected_bots_connection_id_uniq;

ALTER TABLE public.quick_reply_messages
    DROP COLUMN IF EXISTS media;

ALTER TABLE public.business_connected_bots
    DROP COLUMN IF EXISTS connection_id;

ALTER TABLE public.user_business_profiles
    DROP COLUMN IF EXISTS sponsored_messages_enabled;
