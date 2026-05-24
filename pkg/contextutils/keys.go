package contextutils

import (
	"context"
)

type ContextKey string

const UserIDKey ContextKey = "user_id"

// GetUserID retrieves the UserID from a context, returning 0 if not found.
func GetUserID(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	userID, ok := ctx.Value(UserIDKey).(int)
	if !ok {
		return 0
	}
	return userID
}
