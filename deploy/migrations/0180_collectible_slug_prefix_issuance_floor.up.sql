-- A replacement gift may be imported under a new catalog gift_id while
-- retaining the official slug prefix. Serial numbers and slugs are global to
-- that prefix, so bring every active replacement forward to the highest serial
-- already issued by any previous catalog row using the prefix.
ALTER TABLE public.star_gift_collectible_revisions
    DISABLE TRIGGER star_gift_collectible_revision_guard;

WITH active AS (
    SELECT c.collectible_revision_id, r.slug_prefix, r.supply_total, r.reserved
    FROM public.star_gift_catalog c
    JOIN public.star_gift_collectible_revisions r ON r.id=c.collectible_revision_id
    WHERE c.collectible_revision_id IS NOT NULL
), floors AS (
    SELECT active.collectible_revision_id,
           COALESCE((
               SELECT MAX(unique_gift.num)
               FROM public.unique_star_gifts unique_gift
               WHERE unique_gift.slug LIKE active.slug_prefix || '-%'
           ), 0)::integer AS issued_floor,
           active.supply_total,
           active.reserved
    FROM active
)
UPDATE public.star_gift_collectible_revisions revision
SET issued = GREATEST(
    revision.issued,
    LEAST(GREATEST(floors.supply_total - floors.reserved, 0), floors.issued_floor)
)
FROM floors
WHERE revision.id=floors.collectible_revision_id
  AND revision.issued < LEAST(GREATEST(floors.supply_total - floors.reserved, 0), floors.issued_floor);

ALTER TABLE public.star_gift_collectible_revisions
    ENABLE TRIGGER star_gift_collectible_revision_guard;
