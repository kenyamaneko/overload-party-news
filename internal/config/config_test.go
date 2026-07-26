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
	"DATABASE_IAM_AUTH_ENABLED",
	"CLOUDSQL_CONNECTION_NAME",
	"INTERNAL_AUTH_SECRET",
}

func validEnv() map[string]string {
	return map[string]string{
		"ENV":                       "local",
		"INTERNAL_PORT":             "9008",
		"ADMIN_PORT":                "9108",
		"DATABASE_CONN":             "host=localhost dbname=news",
		"DATABASE_IAM_AUTH_ENABLED": "false",
		"INTERNAL_AUTH_SECRET":      "test-internal-auth-secret-do-not-use-in-prod",
	}
}

func TestFromEnv(t *testing.T) {
	t.Run("環境変数からの Config 構築", func(t *testing.T) {
		validCases := []struct {
			name   string
			mutate func(m map[string]string)
		}{
			{
				name:   "全 env が揃うとき、Config が構築される",
				mutate: func(_ map[string]string) {},
			},
			{
				name:   "ENV が staging のとき、Config が構築される",
				mutate: func(m map[string]string) { m["ENV"] = "staging" },
			},
			{
				name:   "ENV が production のとき、Config が構築される",
				mutate: func(m map[string]string) { m["ENV"] = "production" },
			},
			{
				name: "DATABASE_IAM_AUTH_ENABLED が true かつ CLOUDSQL_CONNECTION_NAME が指定されるとき、Config が構築される",
				mutate: func(m map[string]string) {
					m["DATABASE_IAM_AUTH_ENABLED"] = "true"
					m["CLOUDSQL_CONNECTION_NAME"] = "overload-party-dev:asia-northeast1:overload-party-db"
				},
			},
		}
		for _, tc := range validCases {
			t.Run(tc.name, func(t *testing.T) {
				m := validEnv()
				tc.mutate(m)
				applyEnv(t, m)

				cfg, err := config.FromEnv()

				require.NoError(t, err)
				require.NotNil(t, cfg)
			})
		}

		invalidCases := []struct {
			name   string
			mutate func(m map[string]string)
		}{
			{
				name:   "ENV が欠けるとき、エラーになる",
				mutate: func(m map[string]string) { delete(m, "ENV") },
			},
			{
				name:   "ENV が未知値のとき、エラーになる",
				mutate: func(m map[string]string) { m["ENV"] = "dev" },
			},
			{
				name:   "INTERNAL_PORT が欠けるとき、エラーになる",
				mutate: func(m map[string]string) { delete(m, "INTERNAL_PORT") },
			},
			{
				name:   "INTERNAL_PORT が非整数のとき、エラーになる",
				mutate: func(m map[string]string) { m["INTERNAL_PORT"] = "abc" },
			},
			{
				name:   "ADMIN_PORT が欠けるとき、エラーになる",
				mutate: func(m map[string]string) { delete(m, "ADMIN_PORT") },
			},
			{
				name:   "ADMIN_PORT が非整数のとき、エラーになる",
				mutate: func(m map[string]string) { m["ADMIN_PORT"] = "abc" },
			},
			{
				name:   "internal と admin が同ポートのとき、エラーになる",
				mutate: func(m map[string]string) { m["ADMIN_PORT"] = m["INTERNAL_PORT"] },
			},
			{
				name:   "DATABASE_CONN が欠けるとき、エラーになる",
				mutate: func(m map[string]string) { delete(m, "DATABASE_CONN") },
			},
			{
				name:   "DATABASE_IAM_AUTH_ENABLED が欠けるとき、エラーになる",
				mutate: func(m map[string]string) { delete(m, "DATABASE_IAM_AUTH_ENABLED") },
			},
			{
				name:   `DATABASE_IAM_AUTH_ENABLED が "true"/"false" 以外の "yes" のとき、エラーになる`,
				mutate: func(m map[string]string) { m["DATABASE_IAM_AUTH_ENABLED"] = "yes" },
			},
			{
				name: "DATABASE_IAM_AUTH_ENABLED が true かつ CLOUDSQL_CONNECTION_NAME が欠けるとき、エラーになる",
				mutate: func(m map[string]string) {
					m["DATABASE_IAM_AUTH_ENABLED"] = "true"
					delete(m, "CLOUDSQL_CONNECTION_NAME")
				},
			},
			{
				name:   "INTERNAL_AUTH_SECRET が欠けるとき、エラーになる",
				mutate: func(m map[string]string) { delete(m, "INTERNAL_AUTH_SECRET") },
			},
		}
		for _, tc := range invalidCases {
			t.Run(tc.name, func(t *testing.T) {
				m := validEnv()
				tc.mutate(m)
				applyEnv(t, m)

				cfg, err := config.FromEnv()

				require.Error(t, err)
				require.Nil(t, cfg)
			})
		}

		t.Run("全 env が Config の各フィールドに反映される", func(t *testing.T) {
			m := map[string]string{
				"ENV":                       "production",
				"INTERNAL_PORT":             "12345",
				"ADMIN_PORT":                "12346",
				"DATABASE_CONN":             "host=db dbname=news user=n password=p sslmode=disable",
				"DATABASE_IAM_AUTH_ENABLED": "true",
				"CLOUDSQL_CONNECTION_NAME":  "overload-party-dev:asia-northeast1:overload-party-db",
				"INTERNAL_AUTH_SECRET":      "secret-xyz",
			}
			applyEnv(t, m)

			cfg, err := config.FromEnv()

			require.NoError(t, err)
			assert.Equal(t, config.EnvProduction, cfg.Env)
			assert.Equal(t, 12345, cfg.InternalPort)
			assert.Equal(t, 12346, cfg.AdminPort)
			assert.Equal(t, m["DATABASE_CONN"], cfg.DatabaseConn)
			assert.True(t, cfg.DatabaseIAMAuthEnabled)
			assert.Equal(t, m["CLOUDSQL_CONNECTION_NAME"], cfg.CloudSQLConnectionName)
			assert.Equal(t, m["INTERNAL_AUTH_SECRET"], cfg.InternalAuthSecret)
		})

		t.Run("DATABASE_IAM_AUTH_ENABLED が false のとき、CLOUDSQL_CONNECTION_NAME が未設定でも成功する", func(t *testing.T) {
			m := validEnv()
			applyEnv(t, m)

			cfg, err := config.FromEnv()

			require.NoError(t, err)
			assert.False(t, cfg.DatabaseIAMAuthEnabled)
			assert.Empty(t, cfg.CloudSQLConnectionName)
		})
	})
}

// applyEnv は対象 env をいったん空にしてから m の値を設定する。m に無いキーが前テストの
// 値として残らないようにするため、全キーを空にしてから設定する (t.Setenv が自動で cleanup する)。
func applyEnv(t *testing.T, m map[string]string) {
	t.Helper()
	for _, k := range envKeys {
		t.Setenv(k, "")
	}
	for k, v := range m {
		t.Setenv(k, v)
	}
}
