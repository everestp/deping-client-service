package contextutils

import "context"

// unified key type (exported)
type ContextKey string

const (
	UserIDKey    ContextKey = "user_id"
	UserEmailKey ContextKey = "user_email"
)

// GetUserID retrieves user ID from context safely
func GetUserID(ctx context.Context) int {
	if ctx == nil {
		return 0
	}

	v := ctx.Value(UserIDKey)
	id, ok := v.(int)
	if !ok {
		return 0
	}
	return id
}

// GetUserEmail retrieves email from context safely
func GetUserEmail(ctx context.Context) string {
	if ctx == nil {
		return ""
	}

	v := ctx.Value(UserEmailKey)
	email, ok := v.(string)
	if !ok {
		return ""
	}
	return email
}
