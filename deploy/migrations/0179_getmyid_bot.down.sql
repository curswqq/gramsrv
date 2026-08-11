DELETE FROM public.read_model_versions WHERE owner_user_id = 1250000019;
DELETE FROM public.peer_usernames WHERE peer_type = 'user' AND peer_id = 1250000019;
DELETE FROM public.bots WHERE bot_user_id = 1250000019;
DELETE FROM public.users WHERE id = 1250000019;
