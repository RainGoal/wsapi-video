package auth

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Token operations lock the user before the ticket, matching profile changes,
// password changes and token issuance. An old-email ticket cannot verify the
// new address after a concurrent profile change commits.
func consumeToken(ctx context.Context, tx pgx.Tx, token, purpose string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT user_id FROM auth_tokens WHERE token_hash=$1 AND purpose=$2 AND used_at IS NULL AND expires_at>now()`, digest(token), purpose).Scan(&id)
	if err != nil {
		return "", err
	}
	err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 AND status='active' AND ($2='verify' OR email_verified) FOR UPDATE`, id, purpose).Scan(&id)
	if err != nil {
		return "", err
	}
	err = tx.QueryRow(ctx, `UPDATE auth_tokens SET used_at=now() WHERE token_hash=$1 AND user_id=$2 AND purpose=$3 AND used_at IS NULL AND expires_at>now() RETURNING user_id`, digest(token), id, purpose).Scan(&id)
	return id, err
}
