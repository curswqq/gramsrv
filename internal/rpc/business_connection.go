package rpc

import (
	"context"

	"github.com/iamxvbaba/td/tlprofile"

	"telesrv/internal/domain"
)

type businessConnectionContextKey struct{}

type businessConnectionContext struct {
	Connection domain.ConnectedBusinessBot
}

func withBusinessConnection(ctx context.Context, connection domain.ConnectedBusinessBot) context.Context {
	return context.WithValue(ctx, businessConnectionContextKey{}, businessConnectionContext{Connection: connection})
}

func businessConnectionFrom(ctx context.Context) (domain.ConnectedBusinessBot, bool) {
	value, ok := ctx.Value(businessConnectionContextKey{}).(businessConnectionContext)
	if !ok || value.Connection.OwnerUserID == 0 || value.Connection.BotUserID == 0 || value.Connection.ConnectionID == "" {
		return domain.ConnectedBusinessBot{}, false
	}
	return value.Connection, true
}

// businessConnectionMethodAllowed is deliberately a closed allowlist matching
// the official connected-business-bot contract. Rights which can only be
// checked by inspecting the concrete request are checked again in the handler.
func businessConnectionMethodAllowed(method tlprofile.SemanticID, rights domain.BusinessBotRights) bool {
	switch method {
	case tlprofile.SemanticMethodMessagesSendMessage,
		tlprofile.SemanticMethodMessagesSendMedia,
		tlprofile.SemanticMethodMessagesSendMultiMedia,
		tlprofile.SemanticMethodMessagesEditMessage,
		tlprofile.SemanticMethodMessagesSetTyping,
		tlprofile.SemanticMethodMessagesUpdatePinnedMessage:
		return rights.Reply
	case tlprofile.SemanticMethodMessagesReadHistory:
		return rights.ReadMessages
	case tlprofile.SemanticMethodMessagesDeleteMessages:
		// The bare request contains only message IDs, so ownership is not safely
		// knowable at wrapper admission. Requiring both delete capabilities avoids
		// widening either right.
		return rights.DeleteSentMessages && rights.DeleteReceivedMessages
	case tlprofile.SemanticMethodAccountUpdateProfile:
		return rights.EditName || rights.EditBio
	case tlprofile.SemanticMethodAccountSetGlobalPrivacySettings:
		return rights.ChangeGiftSettings
	case tlprofile.SemanticMethodPaymentsGetSavedStarGifts:
		return rights.ViewGifts
	case tlprofile.SemanticMethodPaymentsConvertStarGift:
		return rights.SellGifts
	case tlprofile.SemanticMethodPaymentsTransferStarGift,
		tlprofile.SemanticMethodPaymentsUpgradeStarGift:
		return rights.TransferAndUpgradeGifts
	case tlprofile.SemanticMethodPaymentsGetStarsStatus,
		tlprofile.SemanticMethodPaymentsExportInvoice,
		tlprofile.SemanticMethodPaymentsGetPaymentForm,
		tlprofile.SemanticMethodPaymentsSendStarsForm:
		return rights.TransferStars
	case tlprofile.SemanticMethodStoriesDeleteStories:
		return rights.ManageStories
	default:
		return false
	}
}

func (r *Router) resolveBusinessConnectionForBot(ctx context.Context, connectionID string, botUserID int64) (domain.ConnectedBusinessBot, error) {
	service, ok := r.accountBusinessAutomation()
	if !ok || connectionID == "" {
		return domain.ConnectedBusinessBot{}, businessConnectionInvalidErr()
	}
	connection, found, err := service.GetConnectedBusinessBotByConnectionID(ctx, connectionID)
	if err != nil {
		return domain.ConnectedBusinessBot{}, internalErr()
	}
	if !found || connection.BotUserID != botUserID {
		return domain.ConnectedBusinessBot{}, businessConnectionInvalidErr()
	}
	return connection, nil
}
