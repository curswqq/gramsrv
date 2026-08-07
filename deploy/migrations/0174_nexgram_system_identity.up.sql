-- Brand the official 777000 service account without changing its protocol
-- identity, support flags, phone, username or access hash.
UPDATE public.users
SET first_name = 'NexGram',
    updated_at = now()
WHERE id = 777000
  AND first_name = 'Telesrv';
