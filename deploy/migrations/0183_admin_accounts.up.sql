CREATE TABLE public.admin_users (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    permissions text[] NOT NULL DEFAULT '{}'::text[],
    active boolean NOT NULL DEFAULT true,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.admin_sessions (
    id text PRIMARY KEY,
    admin_id bigint NOT NULL REFERENCES public.admin_users(id) ON DELETE CASCADE,
    csrf_token text NOT NULL,
    ip_addr text NOT NULL DEFAULT '',
    user_agent text NOT NULL DEFAULT '',
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone
);

CREATE INDEX admin_sessions_admin_active_idx ON public.admin_sessions (admin_id) WHERE revoked_at IS NULL;
CREATE INDEX admin_sessions_expiry_idx ON public.admin_sessions (expires_at);
