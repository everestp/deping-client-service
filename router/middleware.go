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

// JWTMiddleware validates the Authorization: Bearer <token> header.
// On success, it injects the user_id into the request context.
func JWTMiddleware(userService services.UserService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")

			// 1. Validate Header Format
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				respondUnauthorized(w, "missing or invalid authorization header")
				return
			}

			// 2. Extract and Validate Token
			token := strings.TrimPrefix(auth, "Bearer ")
			userID, err := userService.ValidateToken(token)
			if err != nil || userID == 0 {
				respondUnauthorized(w, "invalid or expired token")
				return
			}

			// 3. Inject UserID into request context
			// We use the same key defined in pkg/contextutils to avoid collisions
			ctx := context.WithValue(r.Context(), contextutils.UserIDKey, userID)

			// 4. Pass the modified context forward
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// respondUnauthorized is a helper to standardize error responses
func respondUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	// Assuming dto.ErrorResponse has an 'Error' field
	_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
		Error: msg,
	})
}
