package rpc

import (
	"context"
	"testing"

	"github.com/iamxvbaba/td/clock"
	"github.com/iamxvbaba/td/tg"
	"go.uber.org/zap/zaptest"

	"telesrv/internal/domain"
)

func TestDeletedUserTLProjectionContainsOnlyTombstoneIdentity(t *testing.T) {
	u := domain.User{
		ID: 42, AccessHash: 99, Phone: "secret", FirstName: "Alice", LastName: "Private",
		Username: "released", About: "hidden", Verified: true, PremiumUntil: 2_000_000_000,
		PhotoID: 123, Deleted: true, DeletedAt: 1_800_000_000,
	}
	got := tgUser(u)
	if got.ID != u.ID || !got.Deleted {
		t.Fatalf("deleted user = %+v", got)
	}
	if got.AccessHash != 0 || got.Phone != "" || got.FirstName != "" || got.LastName != "" || got.Username != "" || got.Verified || got.Premium || got.Photo != nil || got.Status != nil || len(got.Usernames) != 0 {
		t.Fatalf("deleted user leaked profile state: %+v", got)
	}
	self := tgSelfUser(u)
	if !self.Deleted || self.Self || self.ID != u.ID {
		t.Fatalf("deleted self projection = %+v", self)
	}
}

func TestFrozenUserTLProjectionIsDeletedStyleWithSnowflakeOnly(t *testing.T) {
	u := domain.User{
		ID: 42, AccessHash: 99, Deleted: true, PublicFrozen: true,
		FrozenBadgeIconDocumentID: 8800001,
		Phone:                     "secret", FirstName: "Alice", Username: "hidden", PhotoID: 123,
	}
	got := tgUser(u)
	icon, ok := got.GetBotVerificationIcon()
	if !got.Deleted || !ok || icon != 8800001 {
		t.Fatalf("frozen user = %+v, icon=%d ok=%v", got, icon, ok)
	}
	if got.Phone != "" || got.FirstName != "" || got.Username != "" || got.Photo != nil || got.Premium || got.Verified {
		t.Fatalf("frozen user leaked durable profile state: %+v", got)
	}
}

func TestFrozenUserFullIsMinimalLocalizedAndAttributed(t *testing.T) {
	r := New(Config{AccountFreezeBadgeBotUserID: 1250000013}, Deps{}, zaptest.NewLogger(t), clock.System)
	u := domain.User{ID: 42, Deleted: true, PublicFrozen: true, FrozenBadgeIconDocumentID: 8800001}
	ctx := WithClientInfo(context.Background(), ClientInfo{LangCode: "ru"})
	full := r.frozenAccountUserFull(ctx, u)
	mark, ok := full.GetBotVerification()
	if !ok || mark.BotID != 1250000013 || mark.Icon != 8800001 || mark.Description != "Аккаунт заморожен." {
		t.Fatalf("frozen userFull mark = %+v ok=%v", mark, ok)
	}
	if full.About != "Аккаунт заморожен." || full.StargiftsCount != 0 || full.ProfilePhoto != nil || full.PersonalPhoto != nil || full.FallbackPhoto != nil {
		t.Fatalf("frozen userFull leaked state: %+v", full)
	}
}

func TestGetFullUserUsesFrozenProjectionWithoutCachedProfileLeak(t *testing.T) {
	viewer := domain.User{ID: 7, FirstName: "Viewer"}
	frozen := domain.User{
		ID: 42, AccessHash: 99, Deleted: true, PublicFrozen: true,
		FrozenBadgeIconDocumentID: 8800001,
		// These values simulate a stale or accidentally enriched object. The
		// frozen response must still be constructed from the minimal shape.
		FirstName: "Must not leak", About: "Must not leak", PhotoID: 123,
	}
	r := New(Config{AccountFreezeBadgeBotUserID: 1250000013}, Deps{
		Users: mapUsersService{users: map[int64]domain.User{viewer.ID: viewer, frozen.ID: frozen}},
	}, zaptest.NewLogger(t), clock.System)
	ctx := WithClientInfo(WithUserID(context.Background(), viewer.ID), ClientInfo{LangCode: "en"})
	res, err := r.onUsersGetFullUser(ctx, &tg.InputUser{UserID: frozen.ID, AccessHash: frozen.AccessHash})
	if err != nil {
		t.Fatalf("users.getFullUser: %v", err)
	}
	gotUser := res.Users[0].(*tg.User)
	if !gotUser.Deleted || gotUser.FirstName != "" || gotUser.Photo != nil {
		t.Fatalf("frozen peer user leaked state: %+v", gotUser)
	}
	mark, ok := res.FullUser.GetBotVerification()
	if !ok || mark.Icon != 8800001 || mark.Description != "The account was frozen." || res.FullUser.About != "The account was frozen." || res.FullUser.ProfilePhoto != nil {
		t.Fatalf("frozen full projection = %+v mark=%+v ok=%v", res.FullUser, mark, ok)
	}
}

func TestGetFullUserRechecksDurableFreezeWhenUserProjectionIsStale(t *testing.T) {
	viewer := domain.User{ID: 7, FirstName: "Viewer"}
	target := domain.User{ID: 42, AccessHash: 99, FirstName: "Must not leak", About: "old bio", PhotoID: 123}
	r := New(Config{AccountFreezeBadgeBotUserID: 1250000013}, Deps{
		Users: mapUsersService{users: map[int64]domain.User{viewer.ID: viewer, target.ID: target}},
		AccountFreeze: fixedAccountFreezeService{freeze: domain.AccountFreeze{
			UserID: target.ID, Frozen: true, BadgeIconDocumentID: 8800001,
		}},
	}, zaptest.NewLogger(t), clock.System)
	ctx := WithClientInfo(WithUserID(context.Background(), viewer.ID), ClientInfo{LangCode: "ru"})
	res, err := r.onUsersGetFullUser(ctx, &tg.InputUser{UserID: target.ID, AccessHash: target.AccessHash})
	if err != nil {
		t.Fatalf("users.getFullUser: %v", err)
	}
	gotUser := res.Users[0].(*tg.User)
	icon, iconOK := gotUser.GetBotVerificationIcon()
	mark, markOK := res.FullUser.GetBotVerification()
	if !gotUser.Deleted || !iconOK || icon != 8800001 || gotUser.FirstName != "" || gotUser.Photo != nil {
		t.Fatalf("authoritative frozen user = %+v icon=%d ok=%v", gotUser, icon, iconOK)
	}
	if !markOK || mark.Icon != 8800001 || mark.Description != "Аккаунт заморожен." || res.FullUser.About != "Аккаунт заморожен." {
		t.Fatalf("authoritative frozen full = %+v mark=%+v ok=%v", res.FullUser, mark, markOK)
	}
}

type fixedAccountFreezeService struct {
	freeze domain.AccountFreeze
}

func (s fixedAccountFreezeService) AccountFreeze(_ context.Context, userID int64) (domain.AccountFreeze, bool, error) {
	if s.freeze.Frozen && s.freeze.UserID == userID {
		return s.freeze, true, nil
	}
	return domain.AccountFreeze{}, false, nil
}

func TestHistoryHydrationReplacesStaleUserWithDeletedTombstone(t *testing.T) {
	viewer := domain.User{ID: 7, FirstName: "Viewer"}
	deleted := domain.User{ID: 42, AccessHash: 99, Deleted: true, DeletedAt: 1_800_000_000}
	r := New(Config{}, Deps{Users: mapUsersService{users: map[int64]domain.User{
		viewer.ID: viewer, deleted.ID: deleted,
	}}}, zaptest.NewLogger(t), clock.System)

	list := r.enrichMessageList(context.Background(), viewer.ID, domain.MessageList{
		Messages: []domain.Message{{
			OwnerUserID: viewer.ID,
			Peer:        domain.Peer{Type: domain.PeerTypeUser, ID: deleted.ID},
			From:        domain.Peer{Type: domain.PeerTypeUser, ID: deleted.ID},
			Body:        "retained history",
		}},
		// Simulate an old denormalized message query row. The authoritative
		// Users.ByIDs hydration must replace it, not keep an empty active user.
		Users: []domain.User{{ID: deleted.ID, Phone: "stale", FirstName: "Stale"}},
	})
	if len(list.Users) != 1 || !list.Users[0].Deleted || list.Users[0].Phone != "" || list.Users[0].FirstName != "" {
		t.Fatalf("history users = %+v, want authoritative tombstone", list.Users)
	}
	if got := tgUser(list.Users[0]); !got.Deleted || got.ID != deleted.ID {
		t.Fatalf("history TL user = %+v", got)
	}
}
