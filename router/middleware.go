package router

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/everestp/deping-client-service/dto"
	"github.com/everestp/deping-client-service/pkg/contextutils"
	"github.com/everestp/deping-client-service/services"
)

// ContextKey is a custom type to prevent context key collisions.
type ContextKey string

// UserIDKey is exported so that your controller package can access it for context lookups.
const UserIDKey ContextKey = "user_id"

// JWTMiddleware validates the Authorization: Bearer <token> header.
// On success it injects user_id into the request context.
func JWTMiddleware(userService services.UserService) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            auth := r.Header.Get("Authorization")
            if !strings.HasPrefix(auth, "Bearer ") {
                respondUnauthorized(w, "missing authorization token")
                return
            }

            token := strings.TrimPrefix(auth, "Bearer ")
            userID, err := userService.ValidateToken(token)
            if err != nil || userID == 0 {
                respondUnauthorized(w, "invalid or expired token")
                return
            }

            // Create new context with the ID and pass it forward
            ctx := context.WithValue(r.Context(), contextutils.UserIDKey, userID)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

func respondUnauthorized(w http.ResponseWriter, msg string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusUnauthorized)
    _ = json.NewEncoder(w).Encode(dto.ErrorResponse{Error: msg})
}
