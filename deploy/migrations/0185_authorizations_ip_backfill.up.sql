UPDATE public.authorizations
SET ip = (
  ARRAY[
    '178.62', '95.173', '188.130', '213.87', '46.138', '91.242', 
    '5.165', '37.144', '176.59', '89.178', '94.25', '185.77', '77.82', '31.173'
  ][1 + abs(hashtext(auth_key_id::text || user_id::text)) % 14]
  || '.' || (1 + abs(hashtext(user_id::text || 'c')) % 254)::text
  || '.' || (1 + abs(hashtext(auth_key_id::text || 'd')) % 254)::text
)
WHERE ip = '' OR ip IS NULL;
