package contextutils

type ContextKey string

// UserIDKey is now in a neutral package that everyone can import
const UserIDKey ContextKey = "user_id"
