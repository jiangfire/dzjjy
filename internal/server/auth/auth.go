package auth

import (
	"net/http"
	"strings"
)

// Middleware 认证中间件
type Middleware struct {
	token string
}

// NewMiddleware 创建认证中间件
func NewMiddleware(token string) *Middleware {
	return &Middleware{token: token}
}

// Authenticate 认证处理
func (m *Middleware) Authenticate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != m.token {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}
