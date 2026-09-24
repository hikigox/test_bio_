package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"energy-management/internal/auth"
	"github.com/go-chi/chi/v5"
)

type ctxKey string

const userIDKey ctxKey = "userID"

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// dummyBcryptHash is a precomputed valid bcrypt hash of an arbitrary fixed
// string. It is never a real user's password hash. It exists purely for
// timing safety: when the login lookup fails to find a user, we still run a
// bcrypt compare against this hash so the response takes roughly the same
// time as the "user found, wrong password" path, preventing an attacker
// from enumerating valid emails via response-time measurement.
const dummyBcryptHash = "$2a$10$T2Aq7Ufamudqtnz9YVHytuI.RV.bZc1Pj8xo8Xcfe9luxX72w///S"

func registerAuthRoutes(r chi.Router, s *Server) {
	r.Post("/auth/login", func(w http.ResponseWriter, req *http.Request) {
		var body loginRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "cuerpo inválido")
			return
		}
		var userID int64
		var hash string
		err := s.DB.QueryRow(`SELECT id, password_hash FROM users WHERE email = ?`, body.Email).Scan(&userID, &hash)
		if err != nil {
			hash = dummyBcryptHash
		}
		// Always run the bcrypt compare, even on a lookup miss, so both
		// paths pay the same cost (see dummyBcryptHash above).
		validPassword := auth.VerifyPassword(hash, body.Password)
		if err != nil || !validPassword {
			writeError(w, http.StatusUnauthorized, "credenciales inválidas")
			return
		}
		token, err := auth.GenerateToken(userID, s.JWTSecret)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "error generando token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"token": token})
	})
}

func requireAuth(s *Server) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			h := req.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "falta el token")
				return
			}
			token := strings.TrimPrefix(h, "Bearer ")
			userID, err := auth.ParseToken(token, s.JWTSecret)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "token inválido")
				return
			}
			ctx := context.WithValue(req.Context(), userIDKey, userID)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}
