package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/config"
)

// envKeys は Config が参照する全 env 変数。テスト間の残留を潰すために使う。
var envKeys = []string{
	"ENV",
	"INTERNAL_PORT",
	"ADMIN_PORT",
	"DATABASE_CONN",
	"GOOGLE_CLOUD_PROJECT",
	"NEWS_ARTICLE_COLLECTED_SUBSCRIPTION",
	"INTERNAL_AUTH_SECRET",
}

func validEnv() map[string]string {
	return map[string]string{
		"ENV":                                 "local",
		"INTERNAL_PORT":                       "9008",
		"ADMIN_PORT":                          "9108",
		"DATABASE_CONN":                       "host=localhost dbname=news",
		"GOOGLE_CLOUD_PROJECT":                "news-local",
		"NEWS_ARTICLE_COLLECTED_SUBSCRIPTION": "news-article-collected-news-sub",
		"INTERNAL_AUTH_SECRET":                "test-internal-auth-secret-do-not-use-in-prod",
	}
}

func TestFromEnv_Validation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(m map[string]string)
		wantErr bool
	}{
		{
			name:    "正常",
			mutate:  func(_ map[string]string) {},
			wantErr: false,
		},
		{
			name:    "ENV 欠け",
			mutate:  func(m map[string]string) { delete(m, "ENV") },
			wantErr: true,
		},
		{
			name:    "ENV 未知値",
			mutate:  func(m map[string]string) { m["ENV"] = "dev" },
			wantErr: true,
		},
		{
			name:    "ENV = staging",
			mutate:  func(m map[string]string) { m["ENV"] = "staging" },
			wantErr: false,
		},
		{
			name:    "ENV = production",
			mutate:  func(m map[string]string) { m["ENV"] = "production" },
			wantErr: false,
		},
		{
			name:    "INTERNAL_PORT 欠け",
			mutate:  func(m map[string]string) { delete(m, "INTERNAL_PORT") },
			wantErr: true,
		},
		{
			name:    "INTERNAL_PORT 非整数",
			mutate:  func(m map[string]string) { m["INTERNAL_PORT"] = "abc" },
			wantErr: true,
		},
		{
			name:    "ADMIN_PORT 欠け",
			mutate:  func(m map[string]string) { delete(m, "ADMIN_PORT") },
			wantErr: true,
		},
		{
			name:    "ADMIN_PORT 非整数",
			mutate:  func(m map[string]string) { m["ADMIN_PORT"] = "abc" },
			wantErr: true,
		},
		{
			name:    "internal / admin 同ポートは拒否",
			mutate:  func(m map[string]string) { m["ADMIN_PORT"] = m["INTERNAL_PORT"] },
			wantErr: true,
		},
		{
			name:    "DATABASE_CONN 欠け",
			mutate:  func(m map[string]string) { delete(m, "DATABASE_CONN") },
			wantErr: true,
		},
		{
			name:    "GOOGLE_CLOUD_PROJECT 欠け",
			mutate:  func(m map[string]string) { delete(m, "GOOGLE_CLOUD_PROJECT") },
			wantErr: true,
		},
		{
			name:    "SUBSCRIPTION 欠け",
			mutate:  func(m map[string]string) { delete(m, "NEWS_ARTICLE_COLLECTED_SUBSCRIPTION") },
			wantErr: true,
		},
		{
			name:    "INTERNAL_AUTH_SECRET 欠け",
			mutate:  func(m map[string]string) { delete(m, "INTERNAL_AUTH_SECRET") },
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validEnv()
			tc.mutate(m)
			applyEnv(t, m)

			cfg, err := config.FromEnv()

			assertErrExpectation(t, err, tc.wantErr)
			assert.Equal(t, map[bool]bool{true: true, false: false}[cfg == nil], tc.wantErr)
		})
	}
}

func TestFromEnv_Mapping(t *testing.T) {
	m := map[string]string{
		"ENV":                                 "production",
		"INTERNAL_PORT":                       "12345",
		"ADMIN_PORT":                          "12346",
		"DATABASE_CONN":                       "host=db dbname=news user=n password=p sslmode=disable",
		"GOOGLE_CLOUD_PROJECT":                "proj-42",
		"NEWS_ARTICLE_COLLECTED_SUBSCRIPTION": "sub-name",
		"INTERNAL_AUTH_SECRET":                "secret-xyz",
	}
	applyEnv(t, m)

	cfg, err := config.FromEnv()

	require.NoError(t, err)
	assert.Equal(t, config.EnvProduction, cfg.Env)
	assert.Equal(t, 12345, cfg.InternalPort)
	assert.Equal(t, 12346, cfg.AdminPort)
	assert.Equal(t, m["DATABASE_CONN"], cfg.DatabaseConn)
	assert.Equal(t, m["GOOGLE_CLOUD_PROJECT"], cfg.GoogleCloudProject)
	assert.Equal(t, m["NEWS_ARTICLE_COLLECTED_SUBSCRIPTION"], cfg.NewsArticleCollectedSubscription)
	assert.Equal(t, m["INTERNAL_AUTH_SECRET"], cfg.InternalAuthSecret)
}

// applyEnv は対象 env をいったん空にしてから m の値を設定する。
// テスト間の残留を avoid するために使う (t.Setenv が自動で cleanup する)。
func applyEnv(t *testing.T, m map[string]string) {
	t.Helper()
	for _, k := range envKeys {
		t.Setenv(k, "")
	}
	for k, v := range m {
		t.Setenv(k, v)
	}
}

// assertErrExpectation は wantErr bool を require.Error / require.NoError にテーブル的に分岐する。
// テスト本体に if を書かずケースで条件を表現するためのアサーション wrapper。
func assertErrExpectation(t *testing.T, err error, wantErr bool) {
	t.Helper()
	assert.Equal(t, wantErr, err != nil, "err expectation: wantErr=%v, got=%v", wantErr, err)
}
