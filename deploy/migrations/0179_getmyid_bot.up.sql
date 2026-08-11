-- Built-in @getmyid account identity helper.
INSERT INTO public.users (
    id, access_hash, phone, first_name, last_name, username, country_code,
    created_at, updated_at, verified, support, about, last_seen_at,
    default_history_ttl_period, is_bot, bot_info_version, premium_expires_at,
    emoji_status_document_id, emoji_status_until, color_set, color,
    color_background_emoji_id, profile_color_set, profile_color,
    profile_color_background_emoji_id
) VALUES (
    1250000019, 6845123098451230912, '', 'Get My NexGram ID', '', 'getmyid', '',
    now(), now(), true, false, 'Shows your NexGram account ID.',
    0, 0, true, 1, NULL, 0, 0, false, 0, 0, false, 0, 0
)
ON CONFLICT (id) DO UPDATE SET
    access_hash=EXCLUDED.access_hash, first_name=EXCLUDED.first_name,
    username=EXCLUDED.username, verified=EXCLUDED.verified,
    about=EXCLUDED.about, is_bot=EXCLUDED.is_bot,
    bot_info_version=GREATEST(public.users.bot_info_version, EXCLUDED.bot_info_version),
    updated_at=now();

INSERT INTO public.bots (
    bot_user_id, owner_user_id, token_secret, description, commands,
    bot_chat_history, bot_nochats, inline_placeholder, created_at, updated_at,
    menu_button_type, menu_button_text, menu_button_url, bot_inline_geo
) VALUES (
    1250000019, 1250000019, '', 'Shows your NexGram account ID.',
    '[{"command":"start","description":"Show my NexGram ID"},{"command":"id","description":"Show my NexGram ID"}]'::jsonb,
    false, true, '', now(), now(), 0, '', '', false
)
ON CONFLICT (bot_user_id) DO UPDATE SET
    owner_user_id=EXCLUDED.owner_user_id, description=EXCLUDED.description,
    commands=EXCLUDED.commands, bot_chat_history=EXCLUDED.bot_chat_history,
    bot_nochats=EXCLUDED.bot_nochats, updated_at=now();

INSERT INTO public.peer_usernames
    (username_lower, username, peer_type, peer_id, active, editable, sort_order, updated_at)
VALUES ('getmyid', 'getmyid', 'user', 1250000019, true, false, 0, now())
ON CONFLICT (username_lower) DO UPDATE SET
    username=EXCLUDED.username, peer_type=EXCLUDED.peer_type, peer_id=EXCLUDED.peer_id,
    active=EXCLUDED.active, editable=EXCLUDED.editable, updated_at=now();

INSERT INTO public.read_model_versions (model, owner_user_id, peer_type, peer_id, version, updated_at, hash)
VALUES
    ('contact_account', 1250000019, 'user', 1250000019, 1, now(), 2500001900001),
    ('channel_active_memberships', 1250000019, 'user', 1250000019, 1, now(), 2500001900002)
ON CONFLICT (model, owner_user_id, peer_type, peer_id) DO UPDATE SET
    version=GREATEST(public.read_model_versions.version, EXCLUDED.version),
    updated_at=now(), hash=EXCLUDED.hash;
