package rpc

import (
	"context"
	"testing"

	"github.com/iamxvbaba/td/clock"
	"github.com/iamxvbaba/td/tg"
	"go.uber.org/zap/zaptest"
	"telesrv/internal/domain"
)

type responseFreezeService struct {
	items      map[int64]domain.AccountFreeze
	batchCalls int
}

func TestBuildOutboxUpdatesKeepsFrozenSnowflake(t *testing.T) {
	const (
		viewerID = int64(1001)
		frozenID = int64(1002)
		iconID   = int64(5431895003821513760)
	)
	service := &responseFreezeService{items: map[int64]domain.AccountFreeze{
		frozenID: {UserID: frozenID, Frozen: true, BadgeIconDocumentID: iconID},
	}}
	router := New(Config{}, Deps{AccountFreeze: service}, zaptest.NewLogger(t), clock.System)
	updates := router.BuildOutboxUpdates(context.Background(), []OutboxUpdateRequest{{
		TargetUserID: viewerID,
		Event: domain.UpdateEvent{
			UserID:   viewerID,
			Type:     domain.UpdateEventNewMessage,
			Pts:      1,
			PtsCount: 1,
			Date:     1_700_000_000,
			Message: domain.Message{
				ID:          1,
				OwnerUserID: viewerID,
				Peer:        domain.Peer{Type: domain.PeerTypeUser, ID: frozenID},
				From:        domain.Peer{Type: domain.PeerTypeUser, ID: frozenID},
				Date:        1_700_000_000,
				Pts:         1,
			},
			Users: []domain.User{{ID: frozenID, FirstName: "Stale"}},
		},
	}})
	if len(updates) != 1 || updates[0] == nil || len(updates[0].Users) != 1 {
		t.Fatalf("updates = %+v, want one projected user", updates)
	}
	user, ok := updates[0].Users[0].(*tg.User)
	if !ok || !user.Deleted {
		t.Fatalf("outbox user = %#v, want deleted tombstone", updates[0].Users[0])
	}
	if icon, ok := user.GetBotVerificationIcon(); !ok || icon != iconID {
		t.Fatalf("outbox bot_verification_icon = %d, %v; want %d, true", icon, ok, iconID)
	}
}

func TestDirectPushKeepsFrozenSnowflake(t *testing.T) {
	const (
		viewerID = int64(1001)
		frozenID = int64(1002)
		iconID   = int64(5431895003821513760)
	)
	service := &responseFreezeService{items: map[int64]domain.AccountFreeze{
		frozenID: {UserID: frozenID, Frozen: true, BadgeIconDocumentID: iconID},
	}}
	sessions := &captureSessions{}
	router := New(Config{}, Deps{AccountFreeze: service, Sessions: sessions}, zaptest.NewLogger(t), clock.System)
	msg := &tg.Updates{Users: []tg.UserClass{&tg.User{ID: frozenID, FirstName: "Stale"}}}
	if sent := router.pushUserMessage(context.Background(), viewerID, "test push", msg); sent != 1 {
		t.Fatalf("sent = %d, want 1", sent)
	}
	user := msg.Users[0].(*tg.User)
	if icon, ok := user.GetBotVerificationIcon(); !user.Deleted || !ok || icon != iconID {
		t.Fatalf("direct push user = %+v, icon=%d ok=%v", user, icon, ok)
	}
}

func (s *responseFreezeService) AccountFreeze(_ context.Context, userID int64) (domain.AccountFreeze, bool, error) {
	freeze, ok := s.items[userID]
	return freeze, ok, nil
}

func (s *responseFreezeService) AccountFreezes(_ context.Context, userIDs []int64) (map[int64]domain.AccountFreeze, error) {
	s.batchCalls++
	out := make(map[int64]domain.AccountFreeze)
	for _, userID := range userIDs {
		if freeze, ok := s.items[userID]; ok {
			out[userID] = freeze
		}
	}
	return out, nil
}

func TestAuthoritativeAccountFreezeProjectionCoversNestedResponseUsers(t *testing.T) {
	const (
		viewerID = int64(1001)
		frozenID = int64(1002)
		iconID   = int64(5431895003821513760)
	)
	service := &responseFreezeService{items: map[int64]domain.AccountFreeze{
		frozenID: {UserID: frozenID, Frozen: true, BadgeIconDocumentID: iconID},
	}}
	router := &Router{deps: Deps{AccountFreeze: service}}
	stale := &tg.User{
		ID:        frozenID,
		FirstName: "Must not survive",
		Premium:   true,
		Photo:     &tg.UserProfilePhoto{PhotoID: 99},
	}
	self := &tg.User{ID: viewerID, FirstName: "Self", Self: true}
	response := &tg.PaymentsSavedStarGifts{Users: []tg.UserClass{stale, self}}

	if err := router.applyAuthoritativeAccountFreezesToResponse(context.Background(), viewerID, response); err != nil {
		t.Fatalf("applyAuthoritativeAccountFreezesToResponse: %v", err)
	}
	if service.batchCalls != 1 {
		t.Fatalf("batch calls = %d, want 1", service.batchCalls)
	}
	if !stale.Deleted || stale.FirstName != "" || stale.Premium || stale.Photo != nil {
		t.Fatalf("frozen user = %+v, want minimal deleted tombstone", stale)
	}
	if icon, ok := stale.GetBotVerificationIcon(); !ok || icon != iconID {
		t.Fatalf("bot_verification_icon = %d, %v; want %d, true", icon, ok, iconID)
	}
	if self.FirstName != "Self" || !self.Self || self.Deleted {
		t.Fatalf("self user mutated: %+v", self)
	}
}

func TestCollectResponseUsersFindsDirectAndNestedUsersOnce(t *testing.T) {
	user := &tg.User{ID: 1002}
	response := &tg.UsersUserFull{
		Users: []tg.UserClass{user, user},
	}
	got := collectResponseUsers(response)
	if len(got) != 2 || got[0] != user || got[1] != user {
		t.Fatalf("collectResponseUsers = %#v, want both response occurrences", got)
	}
}
