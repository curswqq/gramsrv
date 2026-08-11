-- A regular gift purchased while a collectible release is available owns one
-- future upgrade slot. Public inventory therefore consists only of serials
-- that are neither issued nor reserved. This prevents early upgraders from
-- exhausting the release and stranding other already purchased gifts.

ALTER TABLE public.star_gift_collectible_revisions
    DISABLE TRIGGER star_gift_collectible_revision_guard;

ALTER TABLE public.star_gift_collectible_revisions
    DROP CONSTRAINT star_gift_collectible_supply_check;

ALTER TABLE public.star_gift_collectible_revisions
    ADD COLUMN reserved integer DEFAULT 0 NOT NULL;

-- Preserve every active regular gift that was sold before reservations
-- existed. If the published supply is already exhausted, expand only the
-- active release by exactly the number of outstanding ordinary gifts.
WITH outstanding AS (
    SELECT c.collectible_revision_id AS revision_id, COUNT(*)::integer AS amount
    FROM public.peer_star_gifts p
    JOIN public.star_gift_catalog c ON c.gift_id = p.gift_id
    WHERE c.collectible_revision_id IS NOT NULL
      AND p.lifecycle_status = 'active'
      AND p.converted = false
      AND p.unique_gift_id IS NULL
    GROUP BY c.collectible_revision_id
)
UPDATE public.star_gift_collectible_revisions r
SET supply_total = GREATEST(r.supply_total, r.issued + outstanding.amount)
FROM outstanding
WHERE r.id = outstanding.revision_id;

ALTER TABLE public.star_gift_collectible_revisions
    ADD CONSTRAINT star_gift_collectible_supply_check
    CHECK (
        supply_total > 0 AND issued >= 0 AND reserved >= 0 AND
        issued + reserved <= supply_total
    );

CREATE TABLE public.star_gift_collectible_reservations (
    saved_gift_id bigint PRIMARY KEY
        REFERENCES public.peer_star_gifts(id) ON DELETE CASCADE,
    collectible_revision_id bigint NOT NULL
        REFERENCES public.star_gift_collectible_revisions(id) ON DELETE RESTRICT,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX star_gift_collectible_reservations_revision_idx
    ON public.star_gift_collectible_reservations(collectible_revision_id);

CREATE FUNCTION public.telesrv_adjust_collectible_reservation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE public.star_gift_collectible_revisions
        SET reserved = reserved + 1
        WHERE id = NEW.collectible_revision_id
          AND status = 'published'
          AND issued + reserved < supply_total;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'collectible upgrade inventory exhausted';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'UPDATE' THEN
        IF NEW.collectible_revision_id = OLD.collectible_revision_id THEN
            RETURN NEW;
        END IF;
        UPDATE public.star_gift_collectible_revisions
        SET reserved = reserved - 1
        WHERE id = OLD.collectible_revision_id AND reserved > 0;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'collectible reservation accounting underflow';
        END IF;
        UPDATE public.star_gift_collectible_revisions
        SET reserved = reserved + 1
        WHERE id = NEW.collectible_revision_id
          AND status = 'published'
          AND issued + reserved < supply_total;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'replacement collectible upgrade inventory exhausted';
        END IF;
        RETURN NEW;
    END IF;

    UPDATE public.star_gift_collectible_revisions
    SET reserved = reserved - 1
    WHERE id = OLD.collectible_revision_id AND reserved > 0;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'collectible reservation accounting underflow';
    END IF;
    RETURN OLD;
END;
$$;

CREATE TRIGGER star_gift_collectible_reservation_adjust
    AFTER INSERT OR UPDATE OF collectible_revision_id OR DELETE
    ON public.star_gift_collectible_reservations
    FOR EACH ROW EXECUTE FUNCTION public.telesrv_adjust_collectible_reservation();

-- Converting, burning, exporting or upgrading an ordinary gift ends its need
-- for a future serial. Transfers deliberately keep the reservation because
-- the entitlement follows the saved gift, not its current owner.
CREATE FUNCTION public.telesrv_release_inactive_collectible_reservation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.unique_gift_id IS NULL AND (
        NEW.unique_gift_id IS NOT NULL OR
        NEW.converted OR
        NEW.lifecycle_status <> 'active'
    ) THEN
        DELETE FROM public.star_gift_collectible_reservations
        WHERE saved_gift_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER star_gift_collectible_reservation_release
    AFTER UPDATE OF unique_gift_id, converted, lifecycle_status
    ON public.peer_star_gifts
    FOR EACH ROW EXECUTE FUNCTION public.telesrv_release_inactive_collectible_reservation();

-- Reserve the slots repaired above. The trigger performs the same invariant
-- check used for every future purchase.
INSERT INTO public.star_gift_collectible_reservations
    (saved_gift_id, collectible_revision_id)
SELECT p.id, c.collectible_revision_id
FROM public.peer_star_gifts p
JOIN public.star_gift_catalog c ON c.gift_id = p.gift_id
WHERE c.collectible_revision_id IS NOT NULL
  AND p.lifecycle_status = 'active'
  AND p.converted = false
  AND p.unique_gift_id IS NULL
ON CONFLICT (saved_gift_id) DO NOTHING;

-- Published release facts remain immutable. The only legal mutable counters
-- are one reservation adjustment or one issuance step per statement.
CREATE OR REPLACE FUNCTION public.telesrv_guard_collectible_revision() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status = 'published' THEN
            RAISE EXCEPTION 'published collectible revision is immutable';
        END IF;
        RETURN OLD;
    END IF;

    IF OLD.status = 'published' THEN
        IF NEW.gift_id <> OLD.gift_id OR NEW.revision <> OLD.revision OR
           NEW.upgrade_stars <> OLD.upgrade_stars OR NEW.supply_total <> OLD.supply_total OR
           NEW.slug_prefix <> OLD.slug_prefix OR NEW.status <> OLD.status OR
           NEW.created_by <> OLD.created_by OR NEW.command_id <> OLD.command_id OR
           NEW.created_at <> OLD.created_at OR NEW.published_at <> OLD.published_at OR
           NEW.official_gift_id IS DISTINCT FROM OLD.official_gift_id OR
           NEW.source_manifest_sha256 IS DISTINCT FROM OLD.source_manifest_sha256 THEN
            RAISE EXCEPTION 'published collectible revision is immutable';
        END IF;
        IF NOT (
            (NEW.issued = OLD.issued + 1 AND NEW.reserved = OLD.reserved) OR
            (NEW.issued = OLD.issued AND NEW.reserved = OLD.reserved + 1) OR
            (NEW.issued = OLD.issued AND NEW.reserved = OLD.reserved - 1)
        ) THEN
            RAISE EXCEPTION 'published collectible counters must advance exactly once';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE public.star_gift_collectible_revisions
    ENABLE TRIGGER star_gift_collectible_revision_guard;
