package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/internal/server/proxy"
)

// staticUpstream records the path it was asked to serve.
type staticUpstream struct {
	hostPort string
	served   atomic.Value
	calls    atomic.Int32
}

func (u *staticUpstream) HostPort() string { return u.hostPort }

func (u *staticUpstream) Serve(w http.ResponseWriter, r *http.Request) {
	u.calls.Add(1)
	u.served.Store(r.URL.Path)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("served:" + r.URL.Path))
}

func startProxy(t *testing.T, resume proxy.Resumer, touch proxy.AccessTracker) *proxy.Proxy {
	t.Helper()
	p, err := proxy.Start("127.0.0.1", 0, resume, touch)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestNewTokenIsUnguessable(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		token, err := proxy.NewToken()
		require.NoError(t, err)
		assert.False(t, seen[token], "tokens must not repeat")
		seen[token] = true
		assert.GreaterOrEqual(t, len(token), 16)
		for _, ch := range token {
			assert.True(t,
				(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_',
				"token contains a character that is not URL-safe: %q", ch)
		}
	}
}

func TestRegisterRejectsInvalidToken(t *testing.T) {
	p := startProxy(t, nil, nil)
	err := p.Register("short", proxy.Target{AppName: "web"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid access token")
}

func TestServesRegisteredTokenAndStripsPrefix(t *testing.T) {
	p := startProxy(t, nil, nil)
	upstream := &staticUpstream{}
	token, err := proxy.NewToken()
	require.NoError(t, err)
	require.NoError(t, p.Register(token, proxy.Target{AppName: "web", Generation: 1, Upstream: upstream}))

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, proxy.TokenPrefix+token+"/index.html", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "served:/index.html", rec.Body.String())
	assert.Equal(t, "/index.html", upstream.served.Load())
}

func TestServesSiteRootForBareToken(t *testing.T) {
	p := startProxy(t, nil, nil)
	upstream := &staticUpstream{}
	token, err := proxy.NewToken()
	require.NoError(t, err)
	require.NoError(t, p.Register(token, proxy.Target{AppName: "web", Upstream: upstream}))

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, proxy.TokenPrefix+token, nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "served:/", rec.Body.String())
}

func TestUnknownTokenIsNotFound(t *testing.T) {
	p := startProxy(t, nil, nil)
	token, err := proxy.NewToken()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, proxy.TokenPrefix+token+"/", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNonProxyPathIsNotFound(t *testing.T) {
	p := startProxy(t, nil, nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/etc/passwd", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUnregisterRemovesAccess(t *testing.T) {
	p := startProxy(t, nil, nil)
	token, err := proxy.NewToken()
	require.NoError(t, err)
	require.NoError(t, p.Register(token, proxy.Target{AppName: "web", Upstream: &staticUpstream{}}))
	p.Unregister(token)

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, proxy.TokenPrefix+token+"/", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestResumerIsInvokedBeforeProxying(t *testing.T) {
	var resumed atomic.Int32
	p := startProxy(t, func(app string, generation uint64) error {
		resumed.Add(1)
		assert.Equal(t, "web", app)
		assert.Equal(t, uint64(7), generation)
		return nil
	}, nil)
	token, err := proxy.NewToken()
	require.NoError(t, err)
	require.NoError(t, p.Register(token, proxy.Target{AppName: "web", Generation: 7, Upstream: &staticUpstream{}}))

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, proxy.TokenPrefix+token+"/", nil))
	assert.Equal(t, int32(1), resumed.Load())
}

func TestResumeFailureReturnsServiceUnavailable(t *testing.T) {
	p := startProxy(t, func(app string, generation uint64) error {
		return assert.AnError
	}, nil)
	token, err := proxy.NewToken()
	require.NoError(t, err)
	require.NoError(t, p.Register(token, proxy.Target{AppName: "web", Upstream: &staticUpstream{}}))

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, proxy.TokenPrefix+token+"/", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestAccessTrackerRecordsTraffic(t *testing.T) {
	var touched atomic.Int32
	p := startProxy(t, nil, func(app string, generation uint64) {
		touched.Add(1)
	})
	token, err := proxy.NewToken()
	require.NoError(t, err)
	require.NoError(t, p.Register(token, proxy.Target{AppName: "web", Generation: 2, Upstream: &staticUpstream{}}))

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, proxy.TokenPrefix+token+"/", nil))
	assert.Equal(t, int32(1), touched.Load())
}

func TestMissingUpstreamIsUnavailable(t *testing.T) {
	p := startProxy(t, nil, nil)
	token, err := proxy.NewToken()
	require.NoError(t, err)
	require.NoError(t, p.Register(token, proxy.Target{AppName: "web"}))

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, proxy.TokenPrefix+token+"/", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestTokensListsRegistrationForApp(t *testing.T) {
	p := startProxy(t, nil, nil)
	tokenA, err := proxy.NewToken()
	require.NoError(t, err)
	tokenB, err := proxy.NewToken()
	require.NoError(t, err)
	require.NoError(t, p.Register(tokenA, proxy.Target{AppName: "web", Upstream: &staticUpstream{}}))
	require.NoError(t, p.Register(tokenB, proxy.Target{AppName: "other", Upstream: &staticUpstream{}}))

	tokens := p.Tokens("web")
	require.Len(t, tokens, 1)
	assert.Equal(t, tokenA, tokens[0])
	assert.False(t, strings.Contains(tokenB, tokenA))
}
