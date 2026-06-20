// Package config は環境変数からサービス起動に必要な設定を読み込む。
// 必須変数が欠ければ即 fail する (デフォルト値へのフォールバックは行わない)。
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Env はサービスの動作環境を表す enum。
type Env string

const (
	EnvLocal      Env = "local"
	EnvStaging    Env = "staging"
	EnvProduction Env = "production"
)

// Config はサービス全体の起動時設定。読み取り後は immutable として扱う。
type Config struct {
	Env Env

	InternalPort int
	AdminPort    int

	DatabaseConn string

	GoogleCloudProject               string
	NewsArticleCollectedSubscription string

	// InternalAuthSecret は内部サービス間 JWT (HS256) 検証の共有秘密鍵。
	InternalAuthSecret string
}

// FromEnv は process env から Config を構築する。必須変数が未設定ならエラー。
func FromEnv() (*Config, error) {
	env, err := parseEnv(os.Getenv("ENV"))
	if err != nil {
		return nil, err
	}

	internalPort, err := requireInt("INTERNAL_PORT")
	if err != nil {
		return nil, err
	}
	adminPort, err := requireInt("ADMIN_PORT")
	if err != nil {
		return nil, err
	}
	if internalPort == adminPort {
		return nil, fmt.Errorf("INTERNAL_PORT and ADMIN_PORT must differ (both = %d)", internalPort)
	}

	databaseConn, err := requireString("DATABASE_CONN")
	if err != nil {
		return nil, err
	}
	cloudProject, err := requireString("GOOGLE_CLOUD_PROJECT")
	if err != nil {
		return nil, err
	}
	sub, err := requireString("NEWS_ARTICLE_COLLECTED_SUBSCRIPTION")
	if err != nil {
		return nil, err
	}
	internalAuthSecret, err := requireString("INTERNAL_AUTH_SECRET")
	if err != nil {
		return nil, err
	}

	return &Config{
		Env:                              env,
		InternalPort:                     internalPort,
		AdminPort:                        adminPort,
		DatabaseConn:                     databaseConn,
		GoogleCloudProject:               cloudProject,
		NewsArticleCollectedSubscription: sub,
		InternalAuthSecret:               internalAuthSecret,
	}, nil
}

// parseEnv は ENV 文字列を Env enum に変換する。未定義値はエラー。
func parseEnv(s string) (Env, error) {
	switch Env(s) {
	case EnvLocal, EnvStaging, EnvProduction:
		return Env(s), nil
	case "":
		return "", fmt.Errorf("ENV is required")
	default:
		return "", fmt.Errorf("ENV: unsupported value %q", s)
	}
}

// requireString は必須の文字列 env を取得する。空文字列ならエラー。
func requireString(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return v, nil
}

// requireInt は必須の整数 env を取得する。未設定 / 非整数ならエラー。
func requireInt(name string) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: not an integer: %q", name, v)
	}
	return n, nil
}
