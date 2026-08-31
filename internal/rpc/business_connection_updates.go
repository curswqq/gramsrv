package rpc

import (
	"context"

	"github.com/iamxvbaba/td/tg"

	"telesrv/internal/domain"
)

// pushConnectedBusinessIncomingMessage delivers the official MTProto business
// update to the connected bot when a new private message is received or sent.
func (r *Router) pushConnectedBusinessIncomingMessage(ctx context.Context, res domain.SendPrivateTextResult) {
	if r == nil || res.Duplicate {
		return
	}
	if res.RecipientMessage.ID > 0 && !res.RecipientMessage.Out {
		r.pushConnectedBusinessSingleMessage(ctx, res.RecipientMessage)
	}
	if res.SenderMessage.ID > 0 && res.SenderMessage.Out {
		r.pushConnectedBusinessSingleMessage(ctx, res.SenderMessage)
	}
}

func (r *Router) pushConnectedBusinessSingleMessage(ctx context.Context, msg domain.Message) {
	ownerUserID := msg.OwnerUserID
	peerUserID := msg.Peer.ID
	if msg.Peer.Type != domain.PeerTypeUser || ownerUserID == 0 || peerUserID == 0 || ownerUserID == peerUserID {
		return
	}
	svc, ok := r.accountBusinessAutomation()
	if !ok {
		return
	}
	connection, found, err := svc.GetConnectedBusinessBot(ctx, ownerUserID)
	if err != nil || !found || connection.BotUserID == 0 || connection.ConnectionID == "" || !connection.Rights.ReadMessages {
		return
	}
	if msg.ViaBotID == connection.BotUserID {
		return
	}
	state, stateFound, err := svc.GetConnectedBusinessBotPeerState(ctx, ownerUserID, peerUserID)
	if err != nil || (stateFound && (state.Paused || state.Disabled)) {
		return
	}

	existingChat, isContact := r.connectedBusinessPeerFacts(ctx, ownerUserID, peerUserID)
	if !domain.BusinessBotRecipientsMatch(connection.Recipients, existingChat, isContact, peerUserID) {
		return
	}

	tgMsg := tgMessage(msg)
	if tgMsg == nil {
		return
	}
	now := int(r.clock.Now().Unix())
	update := &tg.UpdateBotNewBusinessMessage{
		ConnectionID: connection.ConnectionID,
		Message:      tgMsg,
		Qts:          now,
	}
	if msg.ReplyTo != nil && msg.ReplyTo.MessageID > 0 && r.deps.Messages != nil {
		if replied, getErr := r.deps.Messages.GetMessages(ctx, ownerUserID, []int{msg.ReplyTo.MessageID}); getErr == nil && len(replied.Messages) == 1 {
			if reply := tgMessage(replied.Messages[0]); reply != nil {
				update.SetReplyToMessage(reply)
			}
		}
	}

	users := r.usersForMessageUpdate(ctx, connection.BotUserID, msg)
	if r.deps.Users != nil {
		if owner, ownerFound, ownerErr := r.deps.Users.ByID(ctx, connection.BotUserID, ownerUserID); ownerErr == nil && ownerFound {
			users = appendUniqueTGUser(users, r.tgUser(owner))
		}
	}
	out := &tg.Updates{
		Updates: []tg.UpdateClass{update},
		Users:   users,
		Chats:   []tg.ChatClass{},
		Date:    now,
	}
	r.applyPeerReadModels(ctx, connection.BotUserID, out.Users, out.Chats)
	r.pushUserUpdates(ctx, connection.BotUserID, out)
}

func (r *Router) pushConnectedBusinessEditedMessage(ctx context.Context, res domain.EditMessageResult) {
	if r == nil || len(res.Edited) == 0 {
		return
	}
	svc, ok := r.accountBusinessAutomation()
	if !ok {
		return
	}
	for _, item := range res.Edited {
		msg := item.Message
		ownerUserID := item.UserID
		peerUserID := msg.Peer.ID
		if msg.Peer.Type != domain.PeerTypeUser || ownerUserID == 0 || peerUserID == 0 || ownerUserID == peerUserID {
			continue
		}
		connection, found, err := svc.GetConnectedBusinessBot(ctx, ownerUserID)
		if err != nil || !found || connection.BotUserID == 0 || connection.ConnectionID == "" || !connection.Rights.ReadMessages {
			continue
		}
		state, stateFound, err := svc.GetConnectedBusinessBotPeerState(ctx, ownerUserID, peerUserID)
		if err != nil || (stateFound && (state.Paused || state.Disabled)) {
			continue
		}
		existingChat, isContact := r.connectedBusinessPeerFacts(ctx, ownerUserID, peerUserID)
		if !domain.BusinessBotRecipientsMatch(connection.Recipients, existingChat, isContact, peerUserID) {
			continue
		}
		tgMsg := tgMessage(msg)
		if tgMsg == nil {
			continue
		}
		now := int(r.clock.Now().Unix())
		update := &tg.UpdateBotEditBusinessMessage{
			ConnectionID: connection.ConnectionID,
			Message:      tgMsg,
			Qts:          now,
		}
		if msg.ReplyTo != nil && msg.ReplyTo.MessageID > 0 && r.deps.Messages != nil {
			if replied, getErr := r.deps.Messages.GetMessages(ctx, ownerUserID, []int{msg.ReplyTo.MessageID}); getErr == nil && len(replied.Messages) == 1 {
				if reply := tgMessage(replied.Messages[0]); reply != nil {
					update.SetReplyToMessage(reply)
				}
			}
		}
		users := r.usersForMessageUpdate(ctx, connection.BotUserID, msg)
		if r.deps.Users != nil {
			if owner, ownerFound, ownerErr := r.deps.Users.ByID(ctx, connection.BotUserID, ownerUserID); ownerErr == nil && ownerFound {
				users = appendUniqueTGUser(users, r.tgUser(owner))
			}
		}
		out := &tg.Updates{
			Updates: []tg.UpdateClass{update},
			Users:   users,
			Chats:   []tg.ChatClass{},
			Date:    now,
		}
		r.applyPeerReadModels(ctx, connection.BotUserID, out.Users, out.Chats)
		r.pushUserUpdates(ctx, connection.BotUserID, out)
	}
}

func (r *Router) pushConnectedBusinessDeleteMessagesFromResult(ctx context.Context, res domain.DeleteMessagesResult) {
	if r == nil || len(res.Deleted) == 0 {
		return
	}
	svc, ok := r.accountBusinessAutomation()
	if !ok {
		return
	}
	for _, item := range res.Deleted {
		if item.UserID == 0 || len(item.MessageIDs) == 0 {
			continue
		}
		peer := item.Event.Peer
		if peer.Type != domain.PeerTypeUser || peer.ID == 0 || peer.ID == item.UserID {
			continue
		}
		connection, found, err := svc.GetConnectedBusinessBot(ctx, item.UserID)
		if err != nil || !found || connection.BotUserID == 0 || connection.ConnectionID == "" {
			continue
		}
		state, stateFound, err := svc.GetConnectedBusinessBotPeerState(ctx, item.UserID, peer.ID)
		if err != nil || (stateFound && (state.Paused || state.Disabled)) {
			continue
		}
		existingChat, isContact := r.connectedBusinessPeerFacts(ctx, item.UserID, peer.ID)
		if !domain.BusinessBotRecipientsMatch(connection.Recipients, existingChat, isContact, peer.ID) {
			continue
		}
		now := int(r.clock.Now().Unix())
		update := &tg.UpdateBotDeleteBusinessMessage{
			ConnectionID: connection.ConnectionID,
			Peer:         tgPeer(peer),
			Messages:     append([]int(nil), item.MessageIDs...),
			Qts:          now,
		}
		out := &tg.Updates{
			Updates: []tg.UpdateClass{update},
			Users:   []tg.UserClass{},
			Chats:   []tg.ChatClass{},
			Date:    now,
		}
		r.applyPeerReadModels(ctx, connection.BotUserID, out.Users, out.Chats)
		r.pushUserUpdates(ctx, connection.BotUserID, out)
	}
}

func appendUniqueTGUser(users []tg.UserClass, candidate tg.UserClass) []tg.UserClass {
	if candidate == nil {
		return users
	}
	candidateID := candidate.GetID()
	for _, user := range users {
		if user != nil && user.GetID() == candidateID {
			return users
		}
	}
	return append(users, candidate)
}

