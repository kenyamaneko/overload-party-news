package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/config"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENV", "local")
	t.Setenv("INTERNAL_PORT", "8080")
	t.Setenv("DATABASE_CONN", "postgres://user:pass@localhost:5432/news")
	t.Setenv("DATABASE_IAM_AUTH_ENABLED", "false")
	t.Setenv("INTERNAL_AUTH_PUBLIC_KEY", "-----BEGIN PUBLIC KEY-----\nstub\n-----END PUBLIC KEY-----")
}

func TestConfigFromEnv(t *testing.T) {
	t.Run("[起動設定]起動時設定の読み込み", func(t *testing.T) {
		t.Run("ENV・INTERNAL_PORT・DATABASE_CONN・DATABASE_IAM_AUTH_ENABLED・INTERNAL_AUTH_PUBLIC_KEYが全て有効な値のとき、設定の構築に成功する", func(t *testing.T) {
			setValidEnv(t)

			_, err := config.FromEnv()

			assert.NoError(t, err)
		})

		envSuccessTests := []struct {
			name string
			env  string
		}{
			{"ENVがstagingのときも、設定の構築に成功する", "staging"},
			{"ENVがproductionのときも、設定の構築に成功する", "production"},
		}
		for _, tt := range envSuccessTests {
			t.Run(tt.name, func(t *testing.T) {
				setValidEnv(t)
				t.Setenv("ENV", tt.env)

				_, err := config.FromEnv()

				assert.NoError(t, err)
			})
		}

		t.Run("DATABASE_IAM_AUTH_ENABLEDがtrueでCLOUDSQL_CONNECTION_NAMEも指定されているとき、設定の構築に成功する", func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("DATABASE_IAM_AUTH_ENABLED", "true")
			t.Setenv("CLOUDSQL_CONNECTION_NAME", "my-project:asia-northeast1:news-instance")

			_, err := config.FromEnv()

			assert.NoError(t, err)
		})

		t.Run("DATABASE_IAM_AUTH_ENABLEDがfalseのとき、CLOUDSQL_CONNECTION_NAMEを指定しなくても設定の構築に成功し、対応する設定値は空のままになる", func(t *testing.T) {
			setValidEnv(t)

			cfg, err := config.FromEnv()

			require.NoError(t, err)
			assert.Empty(t, cfg.CloudSQLConnectionName)
		})

		t.Run("構築に成功したとき、各環境変数の値が設定の対応する項目にそのまま反映される", func(t *testing.T) {
			t.Setenv("ENV", "staging")
			t.Setenv("INTERNAL_PORT", "9090")
			t.Setenv("DATABASE_CONN", "postgres://user:pass@db-host:5432/news")
			t.Setenv("DATABASE_IAM_AUTH_ENABLED", "true")
			t.Setenv("CLOUDSQL_CONNECTION_NAME", "my-project:asia-northeast1:news-instance")
			t.Setenv("INTERNAL_AUTH_PUBLIC_KEY", "-----BEGIN PUBLIC KEY-----\nfield-mapping\n-----END PUBLIC KEY-----")

			cfg, err := config.FromEnv()

			require.NoError(t, err)
			assert.Equal(t, config.EnvStaging, cfg.Env)
			assert.Equal(t, 9090, cfg.InternalPort)
			assert.Equal(t, "postgres://user:pass@db-host:5432/news", cfg.DatabaseConn)
			assert.True(t, cfg.DatabaseIAMAuthEnabled)
			assert.Equal(t, "my-project:asia-northeast1:news-instance", cfg.CloudSQLConnectionName)
			assert.Equal(t, "-----BEGIN PUBLIC KEY-----\nfield-mapping\n-----END PUBLIC KEY-----", cfg.InternalAuthPublicKey)
		})

		t.Run("起動時設定の構築に失敗する場合", func(t *testing.T) {
			tests := []struct {
				name            string
				mutate          func(t *testing.T)
				wantErrContains []string
			}{
				{
					name: "ENVを指定しないとき",
					mutate: func(t *testing.T) {
						t.Setenv("ENV", "")
					},
					wantErrContains: []string{"ENV"},
				},
				{
					name: "ENVに未定義の値(dev)を指定したとき",
					mutate: func(t *testing.T) {
						t.Setenv("ENV", "dev")
					},
					wantErrContains: []string{"ENV", "dev"},
				},
				{
					name: "INTERNAL_PORTを指定しないとき",
					mutate: func(t *testing.T) {
						t.Setenv("INTERNAL_PORT", "")
					},
					wantErrContains: []string{"INTERNAL_PORT"},
				},
				{
					name: "INTERNAL_PORTに整数でない値を指定したとき",
					mutate: func(t *testing.T) {
						t.Setenv("INTERNAL_PORT", "not-an-integer")
					},
					wantErrContains: []string{"INTERNAL_PORT", "not-an-integer"},
				},
				{
					name: "DATABASE_CONNを指定しないとき",
					mutate: func(t *testing.T) {
						t.Setenv("DATABASE_CONN", "")
					},
					wantErrContains: []string{"DATABASE_CONN"},
				},
				{
					name: "DATABASE_IAM_AUTH_ENABLEDを指定しないとき",
					mutate: func(t *testing.T) {
						t.Setenv("DATABASE_IAM_AUTH_ENABLED", "")
					},
					wantErrContains: []string{"DATABASE_IAM_AUTH_ENABLED"},
				},
				{
					name: "DATABASE_IAM_AUTH_ENABLEDにtrue/false以外の値(yes)を指定したとき",
					mutate: func(t *testing.T) {
						t.Setenv("DATABASE_IAM_AUTH_ENABLED", "yes")
					},
					wantErrContains: []string{"DATABASE_IAM_AUTH_ENABLED", "yes"},
				},
				{
					name: "DATABASE_IAM_AUTH_ENABLEDがtrueでCLOUDSQL_CONNECTION_NAMEを指定しないとき",
					mutate: func(t *testing.T) {
						t.Setenv("DATABASE_IAM_AUTH_ENABLED", "true")
					},
					wantErrContains: []string{"CLOUDSQL_CONNECTION_NAME"},
				},
				{
					name: "INTERNAL_AUTH_PUBLIC_KEYを指定しないとき",
					mutate: func(t *testing.T) {
						t.Setenv("INTERNAL_AUTH_PUBLIC_KEY", "")
					},
					wantErrContains: []string{"INTERNAL_AUTH_PUBLIC_KEY"},
				},
			}

			for _, tt := range tests {
				t.Run(tt.name+"、設定の構築に失敗する", func(t *testing.T) {
					setValidEnv(t)
					tt.mutate(t)

					_, err := config.FromEnv()

					require.Error(t, err)
					for _, want := range tt.wantErrContains {
						assert.ErrorContains(t, err, want)
					}
				})
			}
		})
	})
}
