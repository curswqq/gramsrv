package domain

import (
	"testing"
	"time"
)

func TestComputeAccountRatingStartsPositiveAndAddsEconomicActivity(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	weights := DefaultAccountRatingWeights()

	initial := ComputeAccountRating(AccountRatingSignals{UserID: 42}, weights, now)
	if initial.Stars != AccountRatingLevelThreshold(1) || initial.Level != 1 {
		t.Fatalf("initial rating = %#v, want the positive level-one baseline", initial)
	}

	afterPurchase := ComputeAccountRating(AccountRatingSignals{UserID: 42, StarsSpent: 40}, weights, now)
	if afterPurchase.Stars <= initial.Stars {
		t.Fatalf("rating did not increase after a gift purchase: initial=%d after=%d", initial.Stars, afterPurchase.Stars)
	}
}

func TestComputeAccountRatingNeverBecomesNegative(t *testing.T) {
	rating := ComputeAccountRating(AccountRatingSignals{
		UserID: 99,
		Scam:   true,
		Fake:   true,
		Manual: -1_000_000,
	}, DefaultAccountRatingWeights(), time.Now())
	if rating.Stars != 0 || rating.Level != 0 {
		t.Fatalf("penalized rating = %#v, want a zero clamp", rating)
	}
}
