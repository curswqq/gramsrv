package bots

import (
	"context"
	"fmt"
	"time"

	"telesrv/internal/domain"
)

func (s *Service) respondAsGetMyID(userID int64, session domain.ClientSessionMetadata) {
	mu := s.serviceBotReplyLock(domain.GetMyIDBotUserID, userID)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	text := fmt.Sprintf("Your NexGram ID: %d\n\nThis is the internal identifier of your NexGram account.", userID)
	if session.PreferredLanguage() == "ru" {
		text = fmt.Sprintf("Ваш NexGram ID: %d\n\nЭто внутренний идентификатор вашего аккаунта NexGram.", userID)
	}
	s.sendServiceBotReply(ctx, domain.GetMyIDBotUserID, userID, botReply{Text: text})
}
