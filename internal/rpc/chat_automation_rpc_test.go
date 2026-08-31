package rpc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iamxvbaba/td/clock"
	"github.com/iamxvbaba/td/tg"
	"github.com/iamxvbaba/td/tgerr"
	"go.uber.org/zap/zaptest"

	accountapp "telesrv/internal/app/account"
	appusers "telesrv/internal/app/users"
	"telesrv/internal/domain"
	"telesrv/internal/store/memory"
)

func TestQuickReplyRPCSaveListAndDeleteMessage(t *testing.T) {
	const userID int64 = domain.UserIDSequenceBase
	ctx := WithSessionID(WithAuthKeyID(WithUserID(context.Background(), userID), [8]byte{1}), 77)
	r, updates := newChatAutomationTestRouter(t)
	verify := newFakeBotVerifications()
	r.deps.BotVerifications = verify
	userStore := memory.NewUserStore()
	self, err := userStore.Create(context.Background(), domain.User{
		AccessHash:   7000001,
		Phone:        "15550007001",
		FirstName:    "Quick",
		PremiumUntil: int(time.Now().Add(time.Hour).Unix()),
	})
	if err != nil || self.ID != userID {
		t.Fatalf("create quick-reply user = %+v err %v, want id %d", self, err, userID)
	}
	r.deps.Users = appusers.NewService(userStore)
	const quickReplyIcon = int64(8800023)
	quickReplyPeer := domain.Peer{Type: domain.PeerTypeUser, ID: userID}
	verify.marks[quickReplyPeer] = domain.CustomVerification{
		VerifierBotID:  777000123,
		Peer:           quickReplyPeer,
		IconDocumentID: quickReplyIcon,
		Description:    "Verified quick-reply peer",
	}

	got, err := r.onMessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:               &tg.InputPeerSelf{},
		Message:            "Saved template",
		RandomID:           12345,
		QuickReplyShortcut: &tg.InputQuickReplyShortcut{Shortcut: "hello"},
	})
	if err != nil {
		t.Fatalf("onMessagesSendMessage quick reply: %v", err)
	}
	result, ok := got.(*tg.Updates)
	if !ok {
		t.Fatalf("result type = %T, want *tg.Updates", got)
	}
	var messageID int
	var shortcutID int
	var sawNew bool
	var sawMessageID bool
	for _, update := range result.Updates {
		switch u := update.(type) {
		case *tg.UpdateMessageID:
			sawMessageID = true
			messageID = u.ID
			if u.RandomID != 12345 {
				t.Fatalf("UpdateMessageID random_id = %d", u.RandomID)
			}
		case *tg.UpdateNewQuickReply:
			sawNew = true
			shortcutID = u.QuickReply.ShortcutID
			if u.QuickReply.Shortcut != "hello" {
				t.Fatalf("UpdateNewQuickReply shortcut = %q", u.QuickReply.Shortcut)
			}
		}
	}
	if !sawMessageID || !sawNew || messageID == 0 || shortcutID == 0 {
		t.Fatalf("updates = %#v, want updateMessageID and updateNewQuickReply", result.Updates)
	}
	if len(updates.events) != 1 || updates.events[0].Type != domain.UpdateEventNewQuickReply {
		t.Fatalf("recorded events = %+v", updates.events)
	}

	list, err := r.onMessagesGetQuickReplies(ctx, 0)
	if err != nil {
		t.Fatalf("onMessagesGetQuickReplies: %v", err)
	}
	replies, ok := list.(*tg.MessagesQuickReplies)
	if !ok || len(replies.QuickReplies) != 1 || len(replies.Messages) != 1 {
		t.Fatalf("quick replies = %#v", list)
	}

	quickReplyMessages, err := r.onMessagesGetQuickReplyMessages(ctx, &tg.MessagesGetQuickReplyMessagesRequest{
		ShortcutID: shortcutID,
	})
	if err != nil {
		t.Fatalf("onMessagesGetQuickReplyMessages: %v", err)
	}
	assertMessagesEnvelopeBotVerificationIcon(t, quickReplyMessages, quickReplyPeer, quickReplyIcon)
	if verify.batchCalls != 0 || verify.peerCalls != 1 {
		t.Fatalf("quick-reply verification reads = batch %d peer %d, want 0/1 for one peer", verify.batchCalls, verify.peerCalls)
	}

	deleted, err := r.onMessagesDeleteQuickReplyMessages(ctx, &tg.MessagesDeleteQuickReplyMessagesRequest{
		ShortcutID: shortcutID,
		ID:         []int{messageID},
	})
	if err != nil {
		t.Fatalf("onMessagesDeleteQuickReplyMessages: %v", err)
	}
	deleteUpdates, ok := deleted.(*tg.Updates)
	if !ok {
		t.Fatalf("delete result type = %T", deleted)
	}
	var sawDelete bool
	for _, update := range deleteUpdates.Updates {
		if u, ok := update.(*tg.UpdateDeleteQuickReplyMessages); ok {
			sawDelete = true
			if u.ShortcutID != shortcutID || len(u.Messages) != 1 || u.Messages[0] != messageID {
				t.Fatalf("delete update = %+v", u)
			}
		}
	}
	if !sawDelete {
		t.Fatalf("delete updates = %#v, want updateDeleteQuickReplyMessages", deleteUpdates.Updates)
	}

	// A shortcut with no remaining messages has no valid top_message and must
	// not be returned: Telegram Desktop treats top_message=0 as a fatal server
	// contract violation while loading shortcuts.
	empty, err := r.onMessagesGetQuickReplies(ctx, 0)
	if err != nil {
		t.Fatalf("get quick replies after deleting final message: %v", err)
	}
	emptyReplies, ok := empty.(*tg.MessagesQuickReplies)
	if !ok || len(emptyReplies.QuickReplies) != 0 || len(emptyReplies.Messages) != 0 {
		t.Fatalf("quick replies after deleting final message = %#v, want empty list", empty)
	}
}

func TestQuickReplyConversionDropsInvalidTopMessage(t *testing.T) {
	got := tgQuickReplies([]domain.QuickReply{
		{ID: 1, Shortcut: "empty", TopMessage: 0, Count: 0},
		{ID: 2, Shortcut: "valid", TopMessage: 7, Count: 1},
	})
	if len(got) != 1 || got[0].ShortcutID != 2 || got[0].TopMessage != 7 {
		t.Fatalf("tgQuickReplies = %+v, want only valid shortcut", got)
	}
}

func TestQuickReplyRPCAddsMessageUsingExistingShortcutID(t *testing.T) {
	const userID int64 = domain.UserIDSequenceBase
	ctx := WithSessionID(WithAuthKeyID(WithUserID(context.Background(), userID), [8]byte{1}), 77)
	r, updates := newChatAutomationTestRouter(t)
	userStore := memory.NewUserStore()
	self, err := userStore.Create(context.Background(), domain.User{
		AccessHash:   7000002,
		Phone:        "15550007002",
		FirstName:    "Quick ID",
		PremiumUntil: int(time.Now().Add(time.Hour).Unix()),
	})
	if err != nil || self.ID != userID {
		t.Fatalf("create quick-reply user = %+v err %v, want id %d", self, err, userID)
	}
	r.deps.Users = appusers.NewService(userStore)

	first, err := r.onMessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:               &tg.InputPeerSelf{},
		Message:            "First template",
		RandomID:           71001,
		QuickReplyShortcut: &tg.InputQuickReplyShortcut{Shortcut: "greeting"},
	})
	if err != nil {
		t.Fatalf("create quick reply: %v", err)
	}
	var shortcutID int
	for _, update := range first.(*tg.Updates).Updates {
		if created, ok := update.(*tg.UpdateNewQuickReply); ok {
			shortcutID = created.QuickReply.ShortcutID
		}
	}
	if shortcutID == 0 {
		t.Fatalf("create updates = %#v, want shortcut id", first)
	}

	second, err := r.onMessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:               &tg.InputPeerSelf{},
		Message:            "Second template",
		RandomID:           71002,
		QuickReplyShortcut: &tg.InputQuickReplyShortcutID{ShortcutID: shortcutID},
	})
	if err != nil {
		t.Fatalf("append quick reply by id: %v", err)
	}
	var sawMessage bool
	for _, update := range second.(*tg.Updates).Updates {
		if saved, ok := update.(*tg.UpdateQuickReplyMessage); ok {
			message, ok := saved.Message.(*tg.Message)
			if !ok || message.QuickReplyShortcutID != shortcutID {
				t.Fatalf("quick reply message = %#v", saved.Message)
			}
			sawMessage = true
		}
	}
	if !sawMessage {
		t.Fatalf("append updates = %#v, want updateQuickReplyMessage", second)
	}
	if len(updates.events) != 2 || updates.events[1].Type != domain.UpdateEventQuickReplyMessage {
		t.Fatalf("recorded events = %+v", updates.events)
	}

	messages, err := r.onMessagesGetQuickReplyMessages(ctx, &tg.MessagesGetQuickReplyMessagesRequest{ShortcutID: shortcutID})
	if err != nil {
		t.Fatalf("get quick reply messages: %v", err)
	}
	if got := messages.(*tg.MessagesMessages); len(got.Messages) != 2 {
		t.Fatalf("messages = %#v, want two templates", got.Messages)
	}

	_, err = r.onMessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:               &tg.InputPeerSelf{},
		Message:            "Missing shortcut",
		RandomID:           71003,
		QuickReplyShortcut: &tg.InputQuickReplyShortcutID{ShortcutID: shortcutID + 1000},
	})
	if !tgerr.Is(err, "SHORTCUT_INVALID") {
		t.Fatalf("missing shortcut err = %v, want SHORTCUT_INVALID", err)
	}
}

func TestBusinessGreetingAndAwayAcceptIOSDefaultRecipients(t *testing.T) {
	const userID int64 = domain.UserIDSequenceBase
	ctx := WithSessionID(WithAuthKeyID(WithUserID(context.Background(), userID), [8]byte{2}), 78)
	r, _ := newChatAutomationTestRouter(t)
	userStore := memory.NewUserStore()
	self, err := userStore.Create(context.Background(), domain.User{
		AccessHash:   7000003,
		Phone:        "15550007003",
		FirstName:    "Business",
		PremiumUntil: int(time.Now().Add(time.Hour).Unix()),
	})
	if err != nil || self.ID != userID {
		t.Fatalf("create business user = %+v err %v, want id %d", self, err, userID)
	}
	r.deps.Users = appusers.NewService(userStore)

	created, err := r.onMessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:               &tg.InputPeerSelf{},
		Message:            "Automatic reply",
		RandomID:           72001,
		QuickReplyShortcut: &tg.InputQuickReplyShortcut{Shortcut: "automatic"},
	})
	if err != nil {
		t.Fatalf("create automatic reply: %v", err)
	}
	var shortcutID int
	for _, update := range created.(*tg.Updates).Updates {
		if value, ok := update.(*tg.UpdateNewQuickReply); ok {
			shortcutID = value.QuickReply.ShortcutID
		}
	}
	if shortcutID == 0 {
		t.Fatalf("create updates = %#v, want shortcut id", created)
	}

	greetingReq := &tg.AccountUpdateBusinessGreetingMessageRequest{}
	greetingReq.SetMessage(tg.InputBusinessGreetingMessage{
		ShortcutID:     shortcutID,
		Recipients:     tg.InputBusinessRecipients{ExcludeSelected: true},
		NoActivityDays: 7,
	})
	if ok, err := r.onAccountUpdateBusinessGreetingMessage(ctx, greetingReq); err != nil || !ok {
		t.Fatalf("update greeting = %v, %v", ok, err)
	}

	awayReq := &tg.AccountUpdateBusinessAwayMessageRequest{}
	awayReq.SetMessage(tg.InputBusinessAwayMessage{
		ShortcutID: shortcutID,
		Schedule:   &tg.BusinessAwayMessageScheduleAlways{},
		Recipients: tg.InputBusinessRecipients{ExcludeSelected: true},
	})
	if ok, err := r.onAccountUpdateBusinessAwayMessage(ctx, awayReq); err != nil || !ok {
		t.Fatalf("update away = %v, %v", ok, err)
	}

	profile, found, err := r.deps.Account.(AccountBusinessAutomationService).GetBusinessProfile(ctx, userID)
	if err != nil || !found {
		t.Fatalf("get business profile found=%v err=%v", found, err)
	}
	if profile.Greeting == nil || profile.Greeting.ShortcutID != shortcutID || !profile.Greeting.Recipients.ExcludeSelected {
		t.Fatalf("greeting = %+v", profile.Greeting)
	}
	if profile.Away == nil || profile.Away.ShortcutID != shortcutID || !profile.Away.Recipients.ExcludeSelected {
		t.Fatalf("away = %+v", profile.Away)
	}

	missingReq := &tg.AccountUpdateBusinessGreetingMessageRequest{}
	missingReq.SetMessage(tg.InputBusinessGreetingMessage{
		ShortcutID:     shortcutID + 1000,
		Recipients:     tg.InputBusinessRecipients{ExcludeSelected: true},
		NoActivityDays: 7,
	})
	if _, err := r.onAccountUpdateBusinessGreetingMessage(ctx, missingReq); !tgerr.Is(err, "SHORTCUT_INVALID") {
		t.Fatalf("missing greeting shortcut err = %v, want SHORTCUT_INVALID", err)
	}
}

func TestQuickReplyMediaSaveAndEdit(t *testing.T) {
	const userID int64 = domain.UserIDSequenceBase
	ctx := WithUserID(context.Background(), userID)
	r, _ := newChatAutomationTestRouter(t)
	userStore := memory.NewUserStore()
	self, err := userStore.Create(context.Background(), domain.User{
		AccessHash:   7000011,
		Phone:        "15550007011",
		FirstName:    "Media",
		PremiumUntil: int(time.Now().Add(time.Hour).Unix()),
	})
	if err != nil || self.ID != userID {
		t.Fatalf("create media quick-reply user = %+v err=%v", self, err)
	}
	r.deps.Users = appusers.NewService(userStore)

	updates, err := r.onMessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
		Peer:               &tg.InputPeerSelf{},
		Media:              &tg.InputMediaContact{PhoneNumber: "+15550100", FirstName: "Support"},
		Message:            "Call us",
		RandomID:           81001,
		QuickReplyShortcut: &tg.InputQuickReplyShortcut{Shortcut: "contact"},
	})
	if err != nil {
		t.Fatalf("save media quick reply: %v", err)
	}
	result := updates.(*tg.Updates)
	var messageID, shortcutID int
	for _, update := range result.Updates {
		switch value := update.(type) {
		case *tg.UpdateMessageID:
			messageID = value.ID
		case *tg.UpdateNewQuickReply:
			shortcutID = value.QuickReply.ShortcutID
		}
	}
	if messageID == 0 || shortcutID == 0 {
		t.Fatalf("save media updates = %#v", result.Updates)
	}

	edit := &tg.MessagesEditMessageRequest{Peer: &tg.InputPeerSelf{}, ID: messageID}
	edit.SetQuickReplyShortcutID(shortcutID)
	edit.SetMessage("Updated contact")
	if _, err := r.onMessagesEditMessage(ctx, edit); err != nil {
		t.Fatalf("edit media quick reply: %v", err)
	}
	messages, err := r.onMessagesGetQuickReplyMessages(ctx, &tg.MessagesGetQuickReplyMessagesRequest{ShortcutID: shortcutID})
	if err != nil {
		t.Fatalf("get media quick reply: %v", err)
	}
	envelope := messages.(*tg.MessagesMessages)
	message := envelope.Messages[0].(*tg.Message)
	if message.Message != "Updated contact" {
		t.Fatalf("edited quick reply body = %q", message.Message)
	}
	media, ok := message.Media.(*tg.MessageMediaContact)
	if !ok || media.PhoneNumber != "+15550100" {
		t.Fatalf("edited quick reply media = %#v", message.Media)
	}
}

func TestBusinessChatLinkRPCs(t *testing.T) {
	const userID int64 = 1000000002
	ctx := WithUserID(context.Background(), userID)
	r, _ := newChatAutomationTestRouter(t)
	r.deps.Users = mapUsersService{users: map[int64]domain.User{
		userID: {
			ID:           userID,
			AccessHash:   2002,
			FirstName:    "Business",
			Username:     "business_slot",
			PremiumUntil: int(time.Now().Add(time.Hour).Unix()),
		},
	}}
	registry := newFakeUsernameRegistry()
	registry.byPeer[domain.Peer{Type: domain.PeerTypeUser, ID: userID}] = []domain.Username{
		{Username: "business_slot", Editable: true, Active: true, SortOrder: 0},
		{Username: "business_collectible", Active: true, SortOrder: 1, CollectibleID: 22},
	}
	r.deps.Usernames = registry

	created, err := r.onAccountCreateBusinessChatLink(ctx, tg.InputBusinessChatLink{
		Message: "Prefilled message",
		Title:   "Support",
	})
	if err != nil {
		t.Fatalf("onAccountCreateBusinessChatLink: %v", err)
	}
	if created.Link == "" || created.Message != "Prefilled message" {
		t.Fatalf("created link = %+v", created)
	}
	slug := strings.TrimPrefix(created.Link, "https://telesrv.net/m/")
	if slug == created.Link || slug == "" {
		t.Fatalf("created link URL = %q, want telesrv.net/m slug", created.Link)
	}
	list, err := r.onAccountGetBusinessChatLinks(ctx)
	if err != nil || len(list.Links) != 1 {
		t.Fatalf("onAccountGetBusinessChatLinks len=%d err=%v", len(list.Links), err)
	}
	resolved, err := r.onAccountResolveBusinessChatLink(ctx, slug)
	if err != nil {
		t.Fatalf("onAccountResolveBusinessChatLink: %v", err)
	}
	peer, ok := resolved.Peer.(*tg.PeerUser)
	if !ok || peer.UserID != userID || resolved.Message != "Prefilled message" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if len(resolved.Users) != 1 {
		t.Fatalf("resolved users = %+v, want one", resolved.Users)
	}
	assertVectorOnlyUsernames(t, "resolved business chat owner", resolved.Users[0].(*tg.User), []string{"business_slot", "business_collectible"})
	if registry.peerCalls != 1 || registry.batchCalls != 0 {
		t.Fatalf("resolved business username reads = peer:%d batch:%d, want 1/0", registry.peerCalls, registry.batchCalls)
	}
	list, err = r.onAccountGetBusinessChatLinks(ctx)
	if err != nil || len(list.Links) != 1 || list.Links[0].Views != 1 {
		t.Fatalf("post-resolve links = %+v err=%v", list, err)
	}
	if deleted, err := r.onAccountDeleteBusinessChatLink(ctx, slug); err != nil || !deleted {
		t.Fatalf("onAccountDeleteBusinessChatLink deleted=%v err=%v", deleted, err)
	}
}

func TestConnectedBusinessBotRPCFlow(t *testing.T) {
	const ownerID int64 = 1000000010
	const peerID int64 = 1000000011
	const botID int64 = 1000000012
	ctx := WithSessionID(WithAuthKeyID(WithUserID(context.Background(), ownerID), [8]byte{2}), 88)
	store := memory.NewPasswordStore()
	updates := &captureUpdates{state: domain.UpdateState{Pts: 20, Date: 1700000000}}
	users := mapUsersService{users: map[int64]domain.User{
		ownerID: {ID: ownerID, AccessHash: 101, FirstName: "Bob"},
		peerID:  {ID: peerID, AccessHash: 102, FirstName: "Alice"},
		botID:   {ID: botID, AccessHash: 103, FirstName: "Echo", Username: "echo_test_bot", Bot: true, BotInfoVersion: 1},
	}}
	r := New(Config{DC: 2, IP: "127.0.0.1", Port: 2398}, Deps{
		Account: accountapp.NewService(store, accountapp.WithBusinessAutomation(store)),
		Users:   users,
		Updates: updates,
	}, zaptest.NewLogger(t), clock.System)

	updateReq := &tg.AccountUpdateConnectedBotRequest{
		Bot:        &tg.InputUser{UserID: botID, AccessHash: 103},
		Recipients: tg.InputBusinessBotRecipients{ExcludeSelected: true},
	}
	updateReq.SetRights(tg.BusinessBotRights{Reply: true})
	if _, err := r.onAccountUpdateConnectedBot(ctx, updateReq); err != nil {
		t.Fatalf("onAccountUpdateConnectedBot: %v", err)
	}
	connected, err := r.onAccountGetConnectedBots(ctx)
	if err != nil {
		t.Fatalf("onAccountGetConnectedBots: %v", err)
	}
	if len(connected.ConnectedBots) != 1 || connected.ConnectedBots[0].BotID != botID || !connected.ConnectedBots[0].Rights.Reply {
		t.Fatalf("connected bots = %+v", connected.ConnectedBots)
	}
	botUser, ok := connected.Users[0].(*tg.User)
	if !ok || !botUser.Bot || !botUser.BotBusiness {
		t.Fatalf("connected bot user = %#v, want bot_business", connected.Users[0])
	}
	connection, found, err := store.GetConnectedBusinessBot(context.Background(), ownerID)
	if err != nil || !found || connection.ConnectionID == "" {
		t.Fatalf("stored business connection = %+v found=%v err=%v", connection, found, err)
	}
	botConnection, err := r.onAccountGetBotBusinessConnection(WithUserID(context.Background(), botID), connection.ConnectionID)
	if err != nil {
		t.Fatalf("onAccountGetBotBusinessConnection: %v", err)
	}
	connectionUpdates, ok := botConnection.(*tg.Updates)
	if !ok || len(connectionUpdates.Updates) != 1 {
		t.Fatalf("bot business connection = %#v", botConnection)
	}
	connectUpdate, ok := connectionUpdates.Updates[0].(*tg.UpdateBotBusinessConnect)
	if !ok || connectUpdate.Connection.ConnectionID != connection.ConnectionID || connectUpdate.Connection.UserID != ownerID {
		t.Fatalf("bot business update = %#v", connectionUpdates.Updates[0])
	}
	if _, err := r.onAccountGetBotBusinessConnection(WithUserID(context.Background(), peerID), connection.ConnectionID); !tgerr.Is(err, "BUSINESS_CONNECTION_NOT_ALLOWED") {
		t.Fatalf("user business connection lookup err=%v, want BUSINESS_CONNECTION_NOT_ALLOWED", err)
	}

	peerSettings, err := r.onMessagesGetPeerSettings(ctx, &tg.InputPeerUser{UserID: peerID, AccessHash: 102})
	if err != nil {
		t.Fatalf("onMessagesGetPeerSettings: %v", err)
	}
	if peerSettings.Settings.BusinessBotID != botID || !peerSettings.Settings.BusinessBotCanReply || peerSettings.Settings.BusinessBotPaused {
		t.Fatalf("peer settings before pause = %+v", peerSettings.Settings)
	}

	if ok, err := r.onAccountToggleConnectedBotPaused(ctx, &tg.AccountToggleConnectedBotPausedRequest{
		Peer:   &tg.InputPeerUser{UserID: peerID, AccessHash: 102},
		Paused: true,
	}); err != nil || !ok {
		t.Fatalf("onAccountToggleConnectedBotPaused = %v,%v", ok, err)
	}
	peerSettings, err = r.onMessagesGetPeerSettings(ctx, &tg.InputPeerUser{UserID: peerID, AccessHash: 102})
	if err != nil {
		t.Fatalf("onMessagesGetPeerSettings paused: %v", err)
	}
	if peerSettings.Settings.BusinessBotID != botID || !peerSettings.Settings.BusinessBotPaused || peerSettings.Settings.BusinessBotCanReply {
		t.Fatalf("peer settings paused = %+v", peerSettings.Settings)
	}

	if ok, err := r.onAccountDisablePeerConnectedBot(ctx, &tg.InputPeerUser{UserID: peerID, AccessHash: 102}); err != nil || !ok {
		t.Fatalf("onAccountDisablePeerConnectedBot = %v,%v", ok, err)
	}
	peerSettings, err = r.onMessagesGetPeerSettings(ctx, &tg.InputPeerUser{UserID: peerID, AccessHash: 102})
	if err != nil {
		t.Fatalf("onMessagesGetPeerSettings disabled: %v", err)
	}
	if peerSettings.Settings.BusinessBotID != 0 || peerSettings.Settings.BusinessBotCanReply || peerSettings.Settings.BusinessBotPaused {
		t.Fatalf("peer settings disabled = %+v", peerSettings.Settings)
	}
}

func TestConnectedBusinessBotDefaultsMissingRightsToReply(t *testing.T) {
	const ownerID int64 = 1000000110
	const peerID int64 = 1000000111
	const botID int64 = 1000000112
	ctx := WithSessionID(WithAuthKeyID(WithUserID(context.Background(), ownerID), [8]byte{3}), 89)
	store := memory.NewPasswordStore()
	users := mapUsersService{users: map[int64]domain.User{
		ownerID: {ID: ownerID, AccessHash: 101, FirstName: "Bob"},
		peerID:  {ID: peerID, AccessHash: 102, FirstName: "Alice"},
		botID:   {ID: botID, AccessHash: 103, FirstName: "Echo", Username: "echo_default_bot", Bot: true, BotInfoVersion: 1},
	}}
	r := New(Config{DC: 2, IP: "127.0.0.1", Port: 2398}, Deps{
		Account: accountapp.NewService(store, accountapp.WithBusinessAutomation(store)),
		Users:   users,
	}, zaptest.NewLogger(t), clock.System)

	if _, err := r.onAccountUpdateConnectedBot(ctx, &tg.AccountUpdateConnectedBotRequest{
		Bot:        &tg.InputUser{UserID: botID, AccessHash: 103},
		Recipients: tg.InputBusinessBotRecipients{ExcludeSelected: true},
	}); err != nil {
		t.Fatalf("onAccountUpdateConnectedBot missing rights: %v", err)
	}
	connected, err := r.onAccountGetConnectedBots(ctx)
	if err != nil {
		t.Fatalf("onAccountGetConnectedBots: %v", err)
	}
	if len(connected.ConnectedBots) != 1 || !connected.ConnectedBots[0].Rights.Reply {
		t.Fatalf("missing rights connected bots = %+v, want reply default", connected.ConnectedBots)
	}

	explicitEmpty := &tg.AccountUpdateConnectedBotRequest{
		Bot:        &tg.InputUser{UserID: botID, AccessHash: 103},
		Recipients: tg.InputBusinessBotRecipients{ExcludeSelected: true},
	}
	explicitEmpty.SetRights(tg.BusinessBotRights{})
	if _, err := r.onAccountUpdateConnectedBot(ctx, explicitEmpty); err != nil {
		t.Fatalf("onAccountUpdateConnectedBot explicit empty rights: %v", err)
	}
	connected, err = r.onAccountGetConnectedBots(ctx)
	if err != nil {
		t.Fatalf("onAccountGetConnectedBots explicit empty: %v", err)
	}
	if len(connected.ConnectedBots) != 1 || connected.ConnectedBots[0].Rights.Reply {
		t.Fatalf("explicit empty rights connected bots = %+v, want reply disabled", connected.ConnectedBots)
	}
}

func newChatAutomationTestRouter(t *testing.T) (*Router, *captureUpdates) {
	t.Helper()
	store := memory.NewPasswordStore()
	updates := &captureUpdates{state: domain.UpdateState{Pts: 10, Date: 1700000000}}
	return New(Config{DC: 2, IP: "127.0.0.1", Port: 2398}, Deps{
		Account: accountapp.NewService(store, accountapp.WithBusinessAutomation(store)),
		Updates: updates,
	}, zaptest.NewLogger(t), clock.System), updates
}
