package rpc

import (
	"testing"

	"github.com/iamxvbaba/td/bin"
	"github.com/iamxvbaba/td/tg"
	"telesrv/internal/domain"
)

// Regression: a quote over plain text carries no entities. The reply-header
// converter used to set the quote_entities flag with an empty vector anyway,
// which the canonical layer-228 encoder rejects ("explicit flag has nil
// interface field"), failing the whole getDialogs response and hiding every
// chat from the affected account.
func TestMessageReplyHeaderPlainQuoteOmitsEmptyQuoteEntities(t *testing.T) {
	peer := domain.Peer{Type: domain.PeerTypeUser, ID: 42}
	withEntities := domain.MessageReply{
		MessageID:     5,
		QuoteText:     "**bold**",
		QuoteEntities: []domain.MessageEntity{{Type: domain.MessageEntityBold, Offset: 0, Length: 8}},
		QuoteOffset:   3,
	}
	header := tgMessageReplyHeader(domain.Message{Peer: peer, ReplyTo: &withEntities})
	replyHeader, ok := header.(*tg.MessageReplyHeader)
	if !ok {
		t.Fatalf("header = %T, want *tg.MessageReplyHeader", header)
	}
	if entities, ok := replyHeader.GetQuoteEntities(); !ok || len(entities) != 1 {
		t.Fatalf("quote entities = %#v ok=%v, want one entity", entities, ok)
	}
	if err := replyHeader.Encode(&bin.Buffer{}); err != nil {
		t.Fatalf("encode header with entities: %v", err)
	}

	plain := domain.MessageReply{MessageID: 6, QuoteText: "just text", QuoteOffset: 0}
	header = tgMessageReplyHeader(domain.Message{Peer: peer, ReplyTo: &plain})
	replyHeader, ok = header.(*tg.MessageReplyHeader)
	if !ok {
		t.Fatalf("header = %T, want *tg.MessageReplyHeader", header)
	}
	if entities, ok := replyHeader.GetQuoteEntities(); ok || entities != nil {
		t.Fatalf("plain quote must not set the quote_entities flag (entities=%#v ok=%v)", entities, ok)
	}
	if text, ok := replyHeader.GetQuoteText(); !ok || text != "just text" {
		t.Fatalf("quote text = %q ok=%v, want preserved", text, ok)
	}
	if err := replyHeader.Encode(&bin.Buffer{}); err != nil {
		t.Fatalf("encode plain-quote header: %v", err)
	}
}
