package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jiangfire/dzjjy/internal/server/auth"
	"github.com/stretchr/testify/assert"
)

// TestMiddleware_Authenticate_Success 测试认证成功
func TestMiddleware_Authenticate_Success(t *testing.T) {
	middleware := auth.NewMiddleware("test-token")

	// 创建测试 handler
	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("OK"))
		assert.NoError(t, err)
	}

	// 创建带认证的请求
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()

	// 执行中间件
	wrappedHandler := middleware.Authenticate(handler)
	wrappedHandler(w, req)

	// 验证
	assert.True(t, handlerCalled, "Handler should be called on successful auth")
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestMiddleware_Authenticate_MissingHeader 测试缺少 Authorization 头
func TestMiddleware_Authenticate_MissingHeader(t *testing.T) {
	middleware := auth.NewMiddleware("test-token")

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	wrappedHandler := middleware.Authenticate(handler)
	wrappedHandler(w, req)

	assert.False(t, handlerCalled, "Handler should not be called")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid authorization header format")
}

// TestMiddleware_Authenticate_InvalidFormat 测试错误的 Authorization 头格式
func TestMiddleware_Authenticate_InvalidFormat(t *testing.T) {
	middleware := auth.NewMiddleware("test-token")

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}

	tests := []struct {
		name   string
		header string
	}{
		{"wrong prefix", "Token test-token"},
		{"no space", "Bearertest-token"},
		{"empty", ""},
		{"just bearer", "Bearer"},
		{"missing token", "Bearer "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", tt.header)
			w := httptest.NewRecorder()

			wrappedHandler := middleware.Authenticate(handler)
			wrappedHandler(w, req)

			assert.False(t, handlerCalled, "Handler should not be called for %s", tt.name)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}

// TestMiddleware_Authenticate_WrongToken 测试错误的 token
func TestMiddleware_Authenticate_WrongToken(t *testing.T) {
	middleware := auth.NewMiddleware("test-token")

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}

	tests := []struct {
		name  string
		token string
	}{
		{"completely wrong", "wrong-token"},
		{"partial match", "test-toke"},
		{"extra chars", "test-tokenx"},
		{"different case", "Test-Token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			w := httptest.NewRecorder()

			wrappedHandler := middleware.Authenticate(handler)
			wrappedHandler(w, req)

			assert.False(t, handlerCalled, "Handler should not be called for %s", tt.name)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "invalid token")
		})
	}
}

// TestMiddleware_Authenticate_TokenWithWhitespace 测试 token 包含空白字符
func TestMiddleware_Authenticate_TokenWithWhitespace(t *testing.T) {
	middleware := auth.NewMiddleware("test-token")

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	// Token with leading/trailing whitespace should be trimmed
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer   test-token  ")
	w := httptest.NewRecorder()

	wrappedHandler := middleware.Authenticate(handler)
	wrappedHandler(w, req)

	assert.True(t, handlerCalled, "Handler should be called with trimmed token")
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestMiddleware_Authenticate_EmptyToken 测试空 token
func TestMiddleware_Authenticate_EmptyToken(t *testing.T) {
	middleware := auth.NewMiddleware("test-token")

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}

	// Empty token after Bearer
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()

	wrappedHandler := middleware.Authenticate(handler)
	wrappedHandler(w, req)

	assert.False(t, handlerCalled, "Handler should not be called for empty token")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestMiddleware_Authenticate_EmptyMiddlewareToken 测试中间件使用空 token
func TestMiddleware_Authenticate_EmptyMiddlewareToken(t *testing.T) {
	middleware := auth.NewMiddleware("")

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	// Empty token should match empty middleware token
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()

	wrappedHandler := middleware.Authenticate(handler)
	wrappedHandler(w, req)

	// Empty token after trimming Bearer prefix
	assert.True(t, handlerCalled, "Handler should be called")
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestMiddleware_Authenticate_CaseSensitive 测试 token 大小写敏感
func TestMiddleware_Authenticate_CaseSensitive(t *testing.T) {
	middleware := auth.NewMiddleware("TestToken")

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	// Different case should fail
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	w := httptest.NewRecorder()

	wrappedHandler := middleware.Authenticate(handler)
	wrappedHandler(w, req)

	assert.False(t, handlerCalled, "Handler should not be called for case mismatch")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestMiddleware_Authenticate_SpecialCharacters 测试特殊字符 token
func TestMiddleware_Authenticate_SpecialCharacters(t *testing.T) {
	middleware := auth.NewMiddleware("test!@#$%^&*()_+-=[]{}|;':\",./<>?`~")

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test!@#$%^&*()_+-=[]{}|;':\",./<>?`~")
	w := httptest.NewRecorder()

	wrappedHandler := middleware.Authenticate(handler)
	wrappedHandler(w, req)

	assert.True(t, handlerCalled, "Handler should be called with special char token")
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestMiddleware_Authenticate_LongToken 测试长 token
func TestMiddleware_Authenticate_LongToken(t *testing.T) {
	longToken := "a" + string(make([]byte, 1000))
	middleware := auth.NewMiddleware(longToken)

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+longToken)
	w := httptest.NewRecorder()

	wrappedHandler := middleware.Authenticate(handler)
	wrappedHandler(w, req)

	assert.True(t, handlerCalled, "Handler should be called with long token")
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestMiddleware_Authenticate_UnicodeToken 测试 Unicode token
func TestMiddleware_Authenticate_UnicodeToken(t *testing.T) {
	middleware := auth.NewMiddleware("测试token🔐")

	handlerCalled := false
	handler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer 测试token🔐")
	w := httptest.NewRecorder()

	wrappedHandler := middleware.Authenticate(handler)
	wrappedHandler(w, req)

	assert.True(t, handlerCalled, "Handler should be called with unicode token")
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestMiddleware_Authenticate_MultipleRequests 测试多个请求
func TestMiddleware_Authenticate_MultipleRequests(t *testing.T) {
	middleware := auth.NewMiddleware("test-token")

	callCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}

	wrappedHandler := middleware.Authenticate(handler)

	// Make 5 successful requests
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		wrappedHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Make 3 failed requests
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		w := httptest.NewRecorder()
		wrappedHandler(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	}

	assert.Equal(t, 5, callCount, "Handler should be called 5 times")
}

// TestMiddleware_Authenticate_HeadersPreserved 测试响应头保留
func TestMiddleware_Authenticate_HeadersPreserved(t *testing.T) {
	middleware := auth.NewMiddleware("test-token")

	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "custom-value")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("response body"))
		assert.NoError(t, err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()

	wrappedHandler := middleware.Authenticate(handler)
	wrappedHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "custom-value", w.Header().Get("X-Custom-Header"))
	assert.Equal(t, "response body", w.Body.String())
}
