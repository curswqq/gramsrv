package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/iamxvbaba/td/tg"
	"github.com/iamxvbaba/td/tlprofile"

	"telesrv/internal/domain"
)

func TestBusinessConnectionMethodRights(t *testing.T) {
	if businessConnectionMethodAllowed(tlprofile.SemanticMethodMessagesSendMessage, domain.BusinessBotRights{}) {
		t.Fatal("sendMessage allowed without reply right")
	}
	if !businessConnectionMethodAllowed(tlprofile.SemanticMethodMessagesSendMessage, domain.BusinessBotRights{Reply: true}) {
		t.Fatal("sendMessage rejected with reply right")
	}
	if businessConnectionMethodAllowed(tlprofile.SemanticMethodMessagesDeleteMessages, domain.BusinessBotRights{DeleteSentMessages: true}) {
		t.Fatal("deleteMessages allowed without both delete rights")
	}
	if !businessConnectionMethodAllowed(tlprofile.SemanticMethodMessagesDeleteMessages, domain.BusinessBotRights{DeleteSentMessages: true, DeleteReceivedMessages: true}) {
		t.Fatal("deleteMessages rejected with both delete rights")
	}
	if businessConnectionMethodAllowed(tlprofile.SemanticMethodContactsGetContacts, domain.BusinessBotRights{Reply: true}) {
		t.Fatal("method outside official business allowlist was accepted")
	}
}

func TestBusinessWorkHoursOpenNow(t *testing.T) {
	hours := &domain.BusinessWorkHours{
		TimezoneID: "UTC",
		WeeklyOpen: []domain.BusinessWeeklyOpen{{StartMinute: 6*24*60 + 23*60, EndMinute: 7*24*60 + 2*60}},
	}
	if !businessWorkHoursOpenNow(hours, time.Date(2026, time.August, 10, 1, 0, 0, 0, time.UTC)) { // Monday
		t.Fatal("Sunday-to-Monday interval not open on Monday 01:00")
	}
	if businessWorkHoursOpenNow(hours, time.Date(2026, time.August, 10, 3, 0, 0, 0, time.UTC)) {
		t.Fatal("Sunday-to-Monday interval still open on Monday 03:00")
	}
}

func TestBusinessBotUpdatesPush(t *testing.T) {
	const ownerID int64 = 1000000001
	const customerID int64 = 1000000002
	const botID int64 = 1000000003

	r, _ := newChatAutomationTestRouter(t)
	sessions := &captureSessions{}
	r.deps.Sessions = sessions
	svc, ok := r.accountBusinessAutomation()
	if !ok {
		t.Fatal("accountBusinessAutomation not configured")
	}

	_, err := svc.SaveConnectedBusinessBot(context.Background(), ownerID, domain.ConnectedBusinessBot{
		ConnectionID: "conn_test_123",
		BotUserID:    botID,
		Recipients:   domain.BusinessBotRecipients{ExistingChats: true, NewChats: true},
		Rights:       domain.BusinessBotRights{Reply: true, ReadMessages: true, DeleteSentMessages: true, DeleteReceivedMessages: true},
	})
	if err != nil {
		t.Fatalf("SaveConnectedBusinessBot: %v", err)
	}

	// 1. Incoming message from customer
	r.pushConnectedBusinessIncomingMessage(context.Background(), domain.SendPrivateTextResult{
		RecipientMessage: domain.Message{
			ID:          10,
			OwnerUserID: ownerID,
			From:        domain.Peer{Type: domain.PeerTypeUser, ID: customerID},
			Peer:        domain.Peer{Type: domain.PeerTypeUser, ID: customerID},
			Body:        "Hello business",
			Date:        1700000000,
		},
	})

	if sessions.userID != botID {
		t.Fatalf("push target user = %d, want %d", sessions.userID, botID)
	}
	updates, ok := sessions.userMessage.(*tg.Updates)
	if !ok || len(updates.Updates) != 1 {
		t.Fatalf("sessions.userMessage = %#v, want 1 update", sessions.userMessage)
	}
	if _, ok := updates.Updates[0].(*tg.UpdateBotNewBusinessMessage); !ok {
		t.Fatalf("update type = %T, want *tg.UpdateBotNewBusinessMessage", updates.Updates[0])
	}

	// 2. Edit message in business chat
	r.pushConnectedBusinessEditedMessage(context.Background(), domain.EditMessageResult{
		OwnerUserID: ownerID,
		Edited: []domain.EditedMessageForUser{{
			UserID: ownerID,
			Message: domain.Message{
				ID:          10,
				OwnerUserID: ownerID,
				From:        domain.Peer{Type: domain.PeerTypeUser, ID: customerID},
				Peer:        domain.Peer{Type: domain.PeerTypeUser, ID: customerID},
				Body:        "Hello business edited",
				EditDate:    1700000010,
			},
		}},
	})

	updates, ok = sessions.userMessage.(*tg.Updates)
	if !ok || len(updates.Updates) != 1 {
		t.Fatalf("edit message update = %#v", sessions.userMessage)
	}
	if _, ok := updates.Updates[0].(*tg.UpdateBotEditBusinessMessage); !ok {
		t.Fatalf("update type = %T, want *tg.UpdateBotEditBusinessMessage", updates.Updates[0])
	}

	// 3. Delete message in business chat
	r.pushConnectedBusinessDeleteMessagesFromResult(context.Background(), domain.DeleteMessagesResult{
		OwnerUserID: ownerID,
		Deleted: []domain.DeletedMessagesForUser{{
			UserID:     ownerID,
			MessageIDs: []int{10},
			Event: domain.UpdateEvent{
				UserID:     ownerID,
				Peer:       domain.Peer{Type: domain.PeerTypeUser, ID: customerID},
				MessageIDs: []int{10},
			},
		}},
	})

	updates, ok = sessions.userMessage.(*tg.Updates)
	if !ok || len(updates.Updates) != 1 {
		t.Fatalf("delete message update = %#v", sessions.userMessage)
	}
	if del, ok := updates.Updates[0].(*tg.UpdateBotDeleteBusinessMessage); !ok || len(del.Messages) != 1 || del.Messages[0] != 10 {
		t.Fatalf("update type = %#v, want *tg.UpdateBotDeleteBusinessMessage with [10]", updates.Updates[0])
	}
}


