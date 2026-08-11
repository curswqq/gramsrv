package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"telesrv/internal/domain"
)

const paidMessageReceiverCommissionPermille int64 = 850

func chargePrivatePaidMessageTx(
	ctx context.Context,
	tx pgx.Tx,
	req domain.SendPrivateTextRequest,
) (*domain.StarsBalance, *domain.StarsBalance, error) {
	if req.PaidMessageStars == 0 {
		return nil, nil, nil
	}
	if req.PaidMessageStars < 0 || req.SenderUserID == req.RecipientUserID || req.RecipientBlocked {
		return nil, nil, fmt.Errorf("charge private paid message: invalid charge scope")
	}

	sender := domain.StarsBalance{UserID: req.SenderUserID}
	if err := tx.QueryRow(ctx, `
SELECT balance, granted
FROM stars_balances
WHERE user_id = $1
FOR UPDATE`, req.SenderUserID).Scan(&sender.Balance, &sender.Granted); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, domain.ErrStarsInsufficient
		}
		return nil, nil, fmt.Errorf("lock private paid-message sender balance: %w", err)
	}
	if sender.Balance < req.PaidMessageStars {
		return nil, nil, domain.ErrStarsInsufficient
	}
	if err := tx.QueryRow(ctx, `
UPDATE stars_balances
SET balance = balance - $2, updated_at = now()
WHERE user_id = $1
RETURNING balance`, req.SenderUserID, req.PaidMessageStars).Scan(&sender.Balance); err != nil {
		return nil, nil, fmt.Errorf("debit private paid-message sender balance: %w", err)
	}
	if err := insertStarsTxn(ctx, tx, req.SenderUserID, -req.PaidMessageStars,
		domain.StarsReasonPaidMessage,
		domain.Peer{Type: domain.PeerTypeUser, ID: req.RecipientUserID},
		req.Date, "Paid message", ""); err != nil {
		return nil, nil, err
	}

	credit := req.PaidMessageStars * paidMessageReceiverCommissionPermille / 1000
	recipient := domain.StarsBalance{UserID: req.RecipientUserID}
	if err := tx.QueryRow(ctx, `
INSERT INTO stars_balances(user_id, balance, granted)
VALUES($1, $2, false)
ON CONFLICT(user_id) DO UPDATE
SET balance = stars_balances.balance + EXCLUDED.balance,
    updated_at = now()
RETURNING balance, granted`, req.RecipientUserID, credit).Scan(&recipient.Balance, &recipient.Granted); err != nil {
		return nil, nil, fmt.Errorf("credit private paid-message recipient balance: %w", err)
	}
	if credit > 0 {
		if err := insertStarsTxn(ctx, tx, req.RecipientUserID, credit,
			domain.StarsReasonPaidMessage,
			domain.Peer{Type: domain.PeerTypeUser, ID: req.SenderUserID},
			req.Date, "Paid message revenue", ""); err != nil {
			return nil, nil, err
		}
	}
	return &sender, &recipient, nil
}
