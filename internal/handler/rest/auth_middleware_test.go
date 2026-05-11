package rest_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/handler/rest"
	"github.com/kenyamaneko/overload-party-news/internal/port"
)

// fakeInternalAuthVerifier は port.InternalAuthVerifier の最小 fake 実装。
type fakeInternalAuthVerifier struct {
	playerID string
	err      error
}

func (f *fakeInternalAuthVerifier) Verify(string) (string, error) {
	return f.playerID, f.err
}

var _ port.InternalAuthVerifier = (*fakeInternalAuthVerifier)(nil)

func newAuthTestEngine(verifier port.InternalAuthVerifier) (*gin.Engine, *string) {
	r := gin.New()
	var observed string
	r.GET("/probe", rest.VerifyInternalAuth(verifier), func(c *gin.Context) {
		observed = c.GetString(rest.PlayerIDContextKey)
		c.Status(http.StatusOK)
	})
	return r, &observed
}

func TestVerifyInternalAuth_Success(t *testing.T) {
	verifier := &fakeInternalAuthVerifier{playerID: "player-123"}
	engine, observed := newAuthTestEngine(verifier)

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set(rest.HeaderName, "any.signed.token")

	rr := httptest.NewRecorder()
	engine.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "player-123", *observed)
}

func TestVerifyInternalAuth_Unauthorized(t *testing.T) {
	cases := []struct {
		name     string
		verifier port.InternalAuthVerifier
		setupReq func(*http.Request)
	}{
		{
			name:     "X-Internal-Auth が欠落していれば 401",
			verifier: &fakeInternalAuthVerifier{playerID: "irrelevant"},
			setupReq: func(*http.Request) {},
		},
		{
			name:     "verifier が error を返すなら 401",
			verifier: &fakeInternalAuthVerifier{err: errors.New("invalid token")},
			setupReq: func(r *http.Request) { r.Header.Set(rest.HeaderName, "any.signed.token") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, observed := newAuthTestEngine(tc.verifier)
			req := httptest.NewRequest(http.MethodGet, "/probe", nil)
			tc.setupReq(req)

			rr := httptest.NewRecorder()
			engine.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusUnauthorized, rr.Code)
			assert.Empty(t, *observed)
		})
	}
}
