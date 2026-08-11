DROP TRIGGER IF EXISTS star_gift_collectible_reservation_release
    ON public.peer_star_gifts;
DROP FUNCTION IF EXISTS public.telesrv_release_inactive_collectible_reservation();
DROP TRIGGER IF EXISTS star_gift_collectible_reservation_adjust
    ON public.star_gift_collectible_reservations;
DROP FUNCTION IF EXISTS public.telesrv_adjust_collectible_reservation();
DROP TABLE IF EXISTS public.star_gift_collectible_reservations;

ALTER TABLE public.star_gift_collectible_revisions
    DISABLE TRIGGER star_gift_collectible_revision_guard;
ALTER TABLE public.star_gift_collectible_revisions
    DROP CONSTRAINT star_gift_collectible_supply_check;
ALTER TABLE public.star_gift_collectible_revisions
    DROP COLUMN reserved;
ALTER TABLE public.star_gift_collectible_revisions
    ADD CONSTRAINT star_gift_collectible_supply_check
    CHECK (supply_total > 0 AND issued >= 0 AND issued <= supply_total);

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
        IF NEW.issued <> OLD.issued + 1 THEN
            RAISE EXCEPTION 'published collectible issuance must advance exactly once';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
ALTER TABLE public.star_gift_collectible_revisions
    ENABLE TRIGGER star_gift_collectible_revision_guard;

-- Supply expansions performed by the up migration are deliberately retained:
-- shrinking below already issued serials would corrupt collectible numbering.
