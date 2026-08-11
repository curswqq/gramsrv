// Command telesrv-repair-star-gifts repairs legacy ordinary gifts after the
// collectible-reservation migration. It is deliberately opt-in: without
// --apply it only prints the number of affected gifts.
package main

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	ratingapp "telesrv/internal/app/rating"
	"telesrv/internal/config"
	"telesrv/internal/domain"
	"telesrv/internal/store/postgres"
)

type reservedGift struct {
	ID                   int64
	RevisionID           int64
	Owner                domain.Peer
	FromUserID           int64
	MsgID                int
	SavedID              int64
	PrepaidUpgradeStars  int64
	RevisionUpgradeStars int64
}

func main() {
	apply := flag.Bool("apply", false, "apply free random upgrades to every outstanding reservation")
	recomputeRatings := flag.Bool("recompute-ratings", false, "recompute every ordinary account rating after repairs")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load configuration: %v", err)
	}
	pool, err := postgres.Open(ctx, cfg.PostgresDSN, postgres.WithMaxConns(4), postgres.WithMinConns(1))
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	defer pool.Close()

	gifts, err := loadReservedGifts(ctx, pool)
	if err != nil {
		log.Fatalf("inspect outstanding gifts: %v", err)
	}
	fmt.Printf("outstanding collectible reservations: %d\n", len(gifts))
	if !*apply {
		fmt.Println("dry run only; pass --apply to upgrade these gifts without charging their owners")
		return
	}
	shuffle(gifts)
	upgrades := postgres.NewStarGiftUpgradeStore(pool, postgres.NewMessageStore(pool), postgres.WithStarGiftLifecyclePolicy(domain.StarGiftLifecyclePolicy{
		TransferStars: cfg.StarGiftTransferStars, DropOriginalDetailsStars: cfg.StarGiftDropOriginalDetailsStars,
		OfferMinStars: cfg.StarGiftOfferMinStars, ExportDelaySeconds: int(cfg.StarGiftExportDelay / time.Second),
		TransferDelaySeconds: int(cfg.StarGiftTransferDelay / time.Second), ResellDelaySeconds: int(cfg.StarGiftResellDelay / time.Second),
		CraftDelaySeconds: int(cfg.StarGiftCraftDelay / time.Second), CraftChancePermille: cfg.StarGiftCraftChancePermille,
	}))

	failed := 0
	for index, gift := range gifts {
		actorUserID, ref, ok := repairReference(gift)
		if !ok {
			failed++
			log.Printf("skip saved_gift_id=%d: unusable owner reference", gift.ID)
			continue
		}
		upgradeStars := gift.RevisionUpgradeStars
		if upgradeStars <= 0 {
			upgradeStars = 1
		}
		tag, err := pool.Exec(ctx, `
UPDATE peer_star_gifts
SET prepaid_upgrade_stars=$2
WHERE id=$1 AND unique_gift_id IS NULL AND NOT converted AND lifecycle_status='active'
  AND EXISTS (SELECT 1 FROM star_gift_collectible_reservations reservation WHERE reservation.saved_gift_id=peer_star_gifts.id)`,
			gift.ID, upgradeStars)
		if err != nil || tag.RowsAffected() != 1 {
			failed++
			log.Printf("prepare saved_gift_id=%d: rows=%d err=%v", gift.ID, tag.RowsAffected(), err)
			continue
		}
		result, upgradeErr := upgrades.UpgradeStarGift(ctx, domain.StarGiftUpgradeRequest{
			UserID: actorUserID, Ref: ref, KeepOriginalDetails: true, RequirePrepaid: true,
			CommandKey: fmt.Sprintf("reservation-repair:%d:%d", gift.ID, gift.RevisionID), Date: int(time.Now().Unix()),
		})
		if upgradeErr != nil {
			failed++
			if _, restoreErr := pool.Exec(ctx, `
UPDATE peer_star_gifts SET prepaid_upgrade_stars=$2
WHERE id=$1 AND unique_gift_id IS NULL`, gift.ID, gift.PrepaidUpgradeStars); restoreErr != nil {
				log.Printf("restore saved_gift_id=%d after %v: %v", gift.ID, upgradeErr, restoreErr)
			} else {
				log.Printf("upgrade saved_gift_id=%d failed: %v", gift.ID, upgradeErr)
			}
			continue
		}
		fmt.Printf("[%d/%d] saved_gift_id=%d -> %s (#%d)\n", index+1, len(gifts), gift.ID, result.Unique.Slug, result.Unique.Num)
	}
	if failed > 0 {
		log.Printf("gift repair completed with %d failure(s)", failed)
	}

	if *recomputeRatings {
		if err := recomputeAllRatings(ctx, pool, cfg); err != nil {
			log.Fatalf("recompute ratings: %v", err)
		}
	}
	if failed > 0 {
		os.Exit(2)
	}
}

func loadReservedGifts(ctx context.Context, pool *pgxpool.Pool) ([]reservedGift, error) {
	rows, err := pool.Query(ctx, `
SELECT saved.id, reservation.collectible_revision_id, saved.owner_peer_type, saved.owner_peer_id,
       saved.from_user_id, saved.msg_id, saved.saved_id, saved.prepaid_upgrade_stars, revision.upgrade_stars
FROM star_gift_collectible_reservations reservation
JOIN peer_star_gifts saved ON saved.id=reservation.saved_gift_id
JOIN star_gift_collectible_revisions revision ON revision.id=reservation.collectible_revision_id
WHERE saved.unique_gift_id IS NULL AND NOT saved.converted AND saved.lifecycle_status='active'
ORDER BY saved.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reservedGift
	for rows.Next() {
		var gift reservedGift
		var ownerType string
		if err := rows.Scan(&gift.ID, &gift.RevisionID, &ownerType, &gift.Owner.ID, &gift.FromUserID,
			&gift.MsgID, &gift.SavedID, &gift.PrepaidUpgradeStars, &gift.RevisionUpgradeStars); err != nil {
			return nil, err
		}
		gift.Owner.Type = domain.PeerType(ownerType)
		out = append(out, gift)
	}
	return out, rows.Err()
}

func repairReference(gift reservedGift) (int64, domain.SavedStarGiftRef, bool) {
	ref := domain.SavedStarGiftRef{Owner: gift.Owner}
	switch gift.Owner.Type {
	case domain.PeerTypeUser:
		ref.MsgID = gift.MsgID
		return gift.Owner.ID, ref, gift.Owner.ID > 0 && ref.Valid()
	case domain.PeerTypeChannel:
		ref.SavedID = gift.SavedID
		return gift.FromUserID, ref, gift.FromUserID > 0 && ref.Valid()
	default:
		return 0, domain.SavedStarGiftRef{}, false
	}
}

func shuffle(items []reservedGift) {
	for i := len(items) - 1; i > 0; i-- {
		n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			panic(err)
		}
		j := int(n.Int64())
		items[i], items[j] = items[j], items[i]
	}
}

func recomputeAllRatings(ctx context.Context, pool *pgxpool.Pool, cfg config.Config) error {
	rows, err := pool.Query(ctx, `SELECT id, is_bot FROM users WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		var bot bool
		if err := rows.Scan(&id, &bot); err != nil {
			rows.Close()
			return err
		}
		if domain.RatableAccount(id, bot) {
			ids = append(ids, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	service := ratingapp.NewService(ratingapp.WithStore(postgres.NewAccountRatingStore(pool)), ratingapp.WithEnabled(true),
		ratingapp.WithWeights(cfg.AccountRatingWeights()), ratingapp.WithPendingDelay(0))
	for _, id := range ids {
		if _, err := service.Recompute(ctx, id); err != nil && !errors.Is(err, domain.ErrAccountRatingAdjustmentInvalid) {
			return fmt.Errorf("user %d: %w", id, err)
		}
	}
	fmt.Printf("recomputed account ratings: %d\n", len(ids))
	return nil
}
