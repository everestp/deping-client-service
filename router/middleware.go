package router

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/everestp/deping-client-service/dto"
	"github.com/golang-jwt/jwt/v5"
)

// =========================
// Context Keys (safe typing)
// =========================

type contextKey string

const (
	ContextUserID    contextKey = "user_id"
	ContextUserEmail contextKey = "user_email"
)

// =========================
// Middleware
// =========================

func JWTMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				respondUnauthorized(w, "missing or malformed authorization header")
				return
			}

			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			})

			if err != nil || !token.Valid {
				respondUnauthorized(w, "invalid or expired token")
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				respondUnauthorized(w, "invalid token claims")
				return
			}

			// =========================
			// SAFE parsing (NO PANIC)
			// =========================

			subRaw, ok := claims["sub"]
			if !ok {
				respondUnauthorized(w, "missing subject")
				return
			}

			var userID int
			switch v := subRaw.(type) {
			case float64:
				userID = int(v)
			case int:
				userID = v
			case int64:
				userID = int(v)
			default:
				respondUnauthorized(w, "invalid subject type")
				return
			}

			email, _ := claims["email"].(string)

			// Inject into context
			ctx := context.WithValue(r.Context(), ContextUserID, userID)
			ctx = context.WithValue(ctx, ContextUserEmail, email)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// =========================
// Helpers
// =========================

func GetUserID(r *http.Request) int {
	v, ok := r.Context().Value(ContextUserID).(int)
	if !ok {
		return 0
	}
	return v
}

func GetUserEmail(r *http.Request) string {
	v, ok := r.Context().Value(ContextUserEmail).(string)
	if !ok {
		return ""
	}
	return v
}

// =========================
// Error Response
// =========================

func respondUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
		Error: msg,
	})
}
