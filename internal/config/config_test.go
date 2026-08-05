package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/config"
)

// testPublicKeyPEM は config が値をそのまま保持することの確認にだけ使うダミー。
// 鍵としての妥当性は検証しないため、PEM の体裁だけ揃えている。
const testPublicKeyPEM = "-----BEGIN PUBLIC KEY-----\ndummy-not-a-real-key\n-----END PUBLIC KEY-----\n"

// envKeys は Config が参照する全 env 変数。テスト間の残留を潰すために使う。
var envKeys = []string{
	"ENV",
	"INTERNAL_PORT",
	"DATABASE_CONN",
	"DATABASE_IAM_AUTH_ENABLED",
	"CLOUDSQL_CONNECTION_NAME",
	"INTERNAL_AUTH_PUBLIC_KEY",
}

func validEnv() map[string]string {
	return map[string]string{
		"ENV":                       "local",
		"INTERNAL_PORT":             "9008",
		"DATABASE_CONN":             "host=localhost dbname=news",
		"DATABASE_IAM_AUTH_ENABLED": "false",
		"INTERNAL_AUTH_PUBLIC_KEY":  testPublicKeyPEM,
	}
}

func TestFromEnv(t *testing.T) {
	t.Run("環境変数からのConfig構築", func(t *testing.T) {
		validCases := []struct {
			name   string
			mutate func(m map[string]string)
		}{
			{
				name:   "全envが揃うとき、Configが構築される",
				mutate: func(_ map[string]string) {},
			},
			{
				name:   "ENVがstagingのとき、Configが構築される",
				mutate: func(m map[string]string) { m["ENV"] = "staging" },
			},
			{
				name:   "ENVがproductionのとき、Configが構築される",
				mutate: func(m map[string]string) { m["ENV"] = "production" },
			},
			{
				name: "DATABASE_IAM_AUTH_ENABLEDがtrueかつCLOUDSQL_CONNECTION_NAMEが指定されるとき、Configが構築される",
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
				name:   "ENVが欠けるとき、エラーになる",
				mutate: func(m map[string]string) { delete(m, "ENV") },
			},
			{
				name:   "ENVが未知値のとき、エラーになる",
				mutate: func(m map[string]string) { m["ENV"] = "dev" },
			},
			{
				name:   "INTERNAL_PORTが欠けるとき、エラーになる",
				mutate: func(m map[string]string) { delete(m, "INTERNAL_PORT") },
			},
			{
				name:   "INTERNAL_PORTが非整数のとき、エラーになる",
				mutate: func(m map[string]string) { m["INTERNAL_PORT"] = "abc" },
			},
			{
				name:   "DATABASE_CONNが欠けるとき、エラーになる",
				mutate: func(m map[string]string) { delete(m, "DATABASE_CONN") },
			},
			{
				name:   "DATABASE_IAM_AUTH_ENABLEDが欠けるとき、エラーになる",
				mutate: func(m map[string]string) { delete(m, "DATABASE_IAM_AUTH_ENABLED") },
			},
			{
				name:   `DATABASE_IAM_AUTH_ENABLEDが "true"/"false" 以外の "yes" のとき、エラーになる`,
				mutate: func(m map[string]string) { m["DATABASE_IAM_AUTH_ENABLED"] = "yes" },
			},
			{
				name: "DATABASE_IAM_AUTH_ENABLEDがtrueかつCLOUDSQL_CONNECTION_NAMEが欠けるとき、エラーになる",
				mutate: func(m map[string]string) {
					m["DATABASE_IAM_AUTH_ENABLED"] = "true"
					delete(m, "CLOUDSQL_CONNECTION_NAME")
				},
			},
			{
				name:   "INTERNAL_AUTH_PUBLIC_KEYが欠けるとき、エラーになる",
				mutate: func(m map[string]string) { delete(m, "INTERNAL_AUTH_PUBLIC_KEY") },
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

		t.Run("全envがConfigの各フィールドに反映される", func(t *testing.T) {
			m := map[string]string{
				"ENV":                       "production",
				"INTERNAL_PORT":             "12345",
				"DATABASE_CONN":             "host=db dbname=news user=n password=p sslmode=disable",
				"DATABASE_IAM_AUTH_ENABLED": "true",
				"CLOUDSQL_CONNECTION_NAME":  "overload-party-dev:asia-northeast1:overload-party-db",
				"INTERNAL_AUTH_PUBLIC_KEY":  testPublicKeyPEM,
			}
			applyEnv(t, m)

			cfg, err := config.FromEnv()

			require.NoError(t, err)
			assert.Equal(t, config.EnvProduction, cfg.Env)
			assert.Equal(t, 12345, cfg.InternalPort)
			assert.Equal(t, m["DATABASE_CONN"], cfg.DatabaseConn)
			assert.True(t, cfg.DatabaseIAMAuthEnabled)
			assert.Equal(t, m["CLOUDSQL_CONNECTION_NAME"], cfg.CloudSQLConnectionName)
			assert.Equal(t, m["INTERNAL_AUTH_PUBLIC_KEY"], cfg.InternalAuthPublicKey)
		})

		t.Run("DATABASE_IAM_AUTH_ENABLEDがfalseのとき、CLOUDSQL_CONNECTION_NAMEが未設定でも成功する", func(t *testing.T) {
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
