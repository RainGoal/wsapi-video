package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

type User struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	DisplayName   string    `json:"display_name"`
	Role          string    `json:"role"`
	Status        string    `json:"status"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

type userKey struct{}

func WithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, userKey{}, user)
}
func UserFrom(ctx context.Context) User { user, _ := ctx.Value(userKey{}).(User); return user }

func ID(prefix string) string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("secure random source unavailable")
	}
	return prefix + "_" + hex.EncodeToString(value[:])
}
