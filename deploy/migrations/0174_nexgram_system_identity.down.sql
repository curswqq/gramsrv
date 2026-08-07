UPDATE public.users
SET first_name = 'Telesrv',
    updated_at = now()
WHERE id = 777000
  AND first_name = 'NexGram';
