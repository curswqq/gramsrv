-- Collectible numbers are unique per catalog gift across every published
-- attribute revision. Older servers reset revisions to issued=0, which made
-- the next grant retry an existing (gift_id, num) and slug. Bring each active
-- revision forward to the global issued number; revisions whose replacement
-- supply is already exhausted are safely marked sold out.
--
-- The normal guard only permits issued=N+1. Temporarily disabling this one
-- trigger keeps the repair O(number of gifts), rather than O(highest serial).
-- Both ALTER statements and the UPDATE run in the migration transaction, so a
-- failure cannot leave the guard disabled.
ALTER TABLE public.star_gift_collectible_revisions
    DISABLE TRIGGER star_gift_collectible_revision_guard;

WITH issuance AS (
    SELECT c.gift_id,
           c.collectible_revision_id,
           GREATEST(
               COALESCE((
                   SELECT MAX(u.num)
                   FROM public.unique_star_gifts u
                   WHERE u.gift_id = c.gift_id
               ), 0),
               COALESCE((
                   SELECT MAX(previous.issued)
                   FROM public.star_gift_collectible_revisions previous
                   WHERE previous.gift_id = c.gift_id
               ), 0)
           )::integer AS issued_floor
    FROM public.star_gift_catalog c
    WHERE c.collectible_revision_id IS NOT NULL
)
UPDATE public.star_gift_collectible_revisions active
SET issued = GREATEST(
    active.issued,
    LEAST(active.supply_total, issuance.issued_floor)
)
FROM issuance
WHERE active.id = issuance.collectible_revision_id
  AND active.issued < LEAST(active.supply_total, issuance.issued_floor);

ALTER TABLE public.star_gift_collectible_revisions
    ENABLE TRIGGER star_gift_collectible_revision_guard;
