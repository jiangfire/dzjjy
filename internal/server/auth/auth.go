package auth

import (
	"crypto/subtle"
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

		// 严格验证 Authorization 头格式
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
			return
		}

		// 提取 token 并去除首尾空格
		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))

		// 使用恒定时间比较防止时序攻击
		// 先比较长度，避免不必要的比较
		if len(token) != len(m.token) || subtle.ConstantTimeCompare([]byte(token), []byte(m.token)) != 1 {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}
