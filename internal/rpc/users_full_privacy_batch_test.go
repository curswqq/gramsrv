package rpc

import (
	"context"
	"testing"

	"github.com/iamxvbaba/td/clock"
	"go.uber.org/zap/zaptest"

	appcontacts "telesrv/internal/app/contacts"
	"telesrv/internal/domain"
	"telesrv/internal/store/memory"
)

type userFullBatchPrivacy struct {
	stubPrivacy
	visible     map[domain.PrivacyKey]bool
	batchCalls  int
	scalarCalls int
	keys        []domain.PrivacyKey
}

func (p *userFullBatchPrivacy) CanSee(_ context.Context, _, _ int64, key domain.PrivacyKey) (bool, error) {
	p.scalarCalls++
	return p.visible[key], nil
}

func (p *userFullBatchPrivacy) CanSeeBatch(_ context.Context, ownerUserIDs []int64, _ int64, keys []domain.PrivacyKey) (map[int64]map[domain.PrivacyKey]bool, error) {
	p.batchCalls++
	p.keys = append([]domain.PrivacyKey(nil), keys...)
	out := make(map[int64]map[domain.PrivacyKey]bool, len(ownerUserIDs))
	for _, ownerID := range ownerUserIDs {
		owner := make(map[domain.PrivacyKey]bool, len(p.visible))
		for key, allowed := range p.visible {
			owner[key] = allowed
		}
		out[ownerID] = owner
	}
	return out, nil
}

type userFullScalarPrivacy struct {
	stubPrivacy
	visible map[domain.PrivacyKey]bool
	calls   int
}

func (p *userFullScalarPrivacy) CanSee(_ context.Context, _, _ int64, key domain.PrivacyKey) (bool, error) {
	p.calls++
	return p.visible[key], nil
}

func TestBuildUserFullProjectionBatchesPrivacyAndFailsClosedOnMissingKeys(t *testing.T) {
	privacy := &userFullBatchPrivacy{visible: map[domain.PrivacyKey]bool{
		domain.PrivacyKeyAbout:         false,
		domain.PrivacyKeyPhoneCall:     true,
		domain.PrivacyKeyPhoneP2P:      false,
		domain.PrivacyKeyVoiceMessages: false,
		domain.PrivacyKeyBirthday:      true,
		// ProfilePhoto and SavedMusic are deliberately absent: batch omissions
		// must stay denied rather than becoming a privacy bypass.
	}}
	r := New(Config{}, Deps{Privacy: privacy}, zaptest.NewLogger(t), clock.System)
	full, err := r.buildUserFullProjection(context.Background(), 10, domain.User{
		ID: 20, FirstName: "Target", About: "private about",
		Birthday: domain.Birthday{Day: 2, Month: 8, Year: 2000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if privacy.batchCalls != 1 || privacy.scalarCalls != 0 {
		t.Fatalf("privacy calls batch=%d scalar=%d, want 1/0", privacy.batchCalls, privacy.scalarCalls)
	}
	if len(privacy.keys) != 7 {
		t.Fatalf("batch keys=%v, want seven UserFull privacy keys", privacy.keys)
	}
	if full.About != "" || !full.PhoneCallsAvailable || !full.PhoneCallsPrivate || !full.VoiceMessagesForbidden {
		t.Fatalf("privacy projection=%+v", full)
	}
	if _, ok := full.GetBirthday(); !ok {
		t.Fatal("allowed birthday omitted")
	}
	if _, ok := full.GetProfilePhoto(); ok {
		t.Fatal("missing profile-photo visibility defaulted to visible")
	}
	if _, ok := full.GetSavedMusic(); ok {
		t.Fatal("missing saved-music visibility defaulted to visible")
	}
}

func TestBuildUserFullProjectionRetainsScalarPrivacyFallback(t *testing.T) {
	privacy := &userFullScalarPrivacy{visible: map[domain.PrivacyKey]bool{
		domain.PrivacyKeyAbout:         true,
		domain.PrivacyKeyPhoneCall:     true,
		domain.PrivacyKeyPhoneP2P:      true,
		domain.PrivacyKeyVoiceMessages: true,
		domain.PrivacyKeyProfilePhoto:  true,
		domain.PrivacyKeySavedMusic:    true,
		domain.PrivacyKeyBirthday:      true,
	}}
	r := New(Config{}, Deps{Privacy: privacy}, zaptest.NewLogger(t), clock.System)
	full, err := r.buildUserFullProjection(context.Background(), 10, domain.User{ID: 20, About: "visible"})
	if err != nil {
		t.Fatal(err)
	}
	if privacy.calls != 7 {
		t.Fatalf("scalar privacy calls=%d, want 7", privacy.calls)
	}
	if full.About != "visible" || !full.PhoneCallsAvailable || full.PhoneCallsPrivate || full.VoiceMessagesForbidden {
		t.Fatalf("scalar privacy projection=%+v", full)
	}
}

func TestBuildUserFullProjectionSelfSkipsPrivacyEvaluation(t *testing.T) {
	privacy := &userFullBatchPrivacy{visible: map[domain.PrivacyKey]bool{}}
	r := New(Config{}, Deps{Privacy: privacy}, zaptest.NewLogger(t), clock.System)
	full, err := r.buildUserFullProjection(context.Background(), 20, domain.User{ID: 20, About: "self"})
	if err != nil {
		t.Fatal(err)
	}
	if privacy.batchCalls != 0 || privacy.scalarCalls != 0 || full.About != "self" {
		t.Fatalf("self projection calls=%d/%d full=%+v", privacy.batchCalls, privacy.scalarCalls, full)
	}
}

func TestBuildUserFullProjectionSuppressesOwnerPhotosAndCallsForBlockedViewer(t *testing.T) {
	ctx := context.Background()
	const (
		ownerID  int64 = 3001
		viewerID int64 = 3002
	)
	contactsStore := memory.NewContactStore()
	if _, err := contactsStore.Block(ctx, ownerID, viewerID, 100); err != nil {
		t.Fatal(err)
	}
	files := &fakeFiles{
		photos: map[int64]domain.Photo{
			9401: {ID: 9401, AccessHash: 1, DCID: 2, Sizes: fakeAvatarStaticSizes()},
			9402: {ID: 9402, AccessHash: 2, DCID: 3, Sizes: fakeAvatarStaticSizes()},
		},
		profile: map[fakeProfilePhotoKey]int64{
			{ownerType: domain.PeerTypeUser, ownerID: ownerID, kind: domain.ProfilePhotoKindProfile}:  9401,
			{ownerType: domain.PeerTypeUser, ownerID: ownerID, kind: domain.ProfilePhotoKindFallback}: 9402,
		},
	}
	r := New(Config{}, Deps{
		Contacts: appcontacts.NewService(contactsStore, memory.NewUserStore()),
		Files:    files,
		Privacy:  stubPrivacy{},
	}, zaptest.NewLogger(t), clock.System)

	full, err := r.buildUserFullProjection(ctx, viewerID, domain.User{ID: ownerID, FirstName: "Owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := full.GetProfilePhoto(); ok {
		t.Fatal("blocked viewer received owner profile photo")
	}
	if _, ok := full.GetFallbackPhoto(); ok {
		t.Fatal("blocked viewer received owner fallback photo")
	}
	if full.PhoneCallsAvailable || full.VideoCallsAvailable || !full.PhoneCallsPrivate || !full.VoiceMessagesForbidden {
		t.Fatalf("blocked call projection = %+v", full)
	}

	if _, err := contactsStore.Unblock(ctx, ownerID, viewerID); err != nil {
		t.Fatal(err)
	}
	restored, err := r.buildUserFullProjection(ctx, viewerID, domain.User{ID: ownerID, FirstName: "Owner"})
	if err != nil {
		t.Fatal(err)
	}
	photo, ok := restored.GetProfilePhoto()
	if !ok || photo.GetID() != 9401 {
		t.Fatalf("restored profile photo = %#v, %v", photo, ok)
	}
	if !restored.PhoneCallsAvailable || !restored.VideoCallsAvailable || restored.PhoneCallsPrivate || restored.VoiceMessagesForbidden {
		t.Fatalf("restored call projection = %+v", restored)
	}
}
