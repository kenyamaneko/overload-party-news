// Package apinewsclient は consumer から wire 詳細 (REST path / HTTP status code) を
// 隠蔽するため、生成 client を sentinel error 変換でラップする。
package apinewsclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// Sentinel errors. wire 契約 (OpenAPI spec) で定義された status code の意味を sentinel として export する。
var (
	// ErrNotFound は status 404。
	ErrNotFound = errors.New("apinewsclient: not found")

	// ErrUnauthorized は status 401。
	ErrUnauthorized = errors.New("apinewsclient: unauthorized")

	// ErrBadRequest は status 400。
	ErrBadRequest = errors.New("apinewsclient: bad request")

	// ErrInternalServer は status 5xx。
	ErrInternalServer = errors.New("apinewsclient: internal server error")
)

// Client は overload-party-news REST API の sentinel-error converted client。
type Client struct {
	api *apinews.ClientWithResponses
}

// Option は Client 構築時の設定。
type Option func(*config)

type config struct {
	httpClient apinews.HttpRequestDoer
	editors    []apinews.RequestEditorFn
}

// WithHTTPClient は基底 HTTP doer を差し替える。デフォルトは http.DefaultClient。
func WithHTTPClient(doer apinews.HttpRequestDoer) Option {
	return func(c *config) { c.httpClient = doer }
}

// WithRequestEditorFn は全リクエストに適用する RequestEditor を追加する。
func WithRequestEditorFn(fn apinews.RequestEditorFn) Option {
	return func(c *config) { c.editors = append(c.editors, fn) }
}

// New は baseURL に接続する Client を生成する。
func New(baseURL string, opts ...Option) (*Client, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	apiOpts := make([]apinews.ClientOption, 0, 1+len(cfg.editors))
	if cfg.httpClient != nil {
		apiOpts = append(apiOpts, apinews.WithHTTPClient(cfg.httpClient))
	}
	for _, editor := range cfg.editors {
		apiOpts = append(apiOpts, apinews.WithRequestEditorFn(editor))
	}

	api, err := apinews.NewClientWithResponses(baseURL, apiOpts...)
	if err != nil {
		return nil, fmt.Errorf("apinewsclient: new: %w", err)
	}
	return &Client{api: api}, nil
}

// GetHealth はサービス死活監視用エンドポイントを叩く。
func (c *Client) GetHealth(ctx context.Context) (*apinews.HealthResponse, error) {
	resp, err := c.api.GetHealthWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("apinewsclient: GetHealth: %w", err)
	}
	if resp.JSON200 != nil {
		return resp.JSON200, nil
	}
	return nil, toStatusError("GetHealth", resp.StatusCode())
}

// ListNews は指定言語で公開中のニュース記事一覧を返す。
func (c *Client) ListNews(ctx context.Context, lang string, limit int) (*apinews.NewsListResponse, error) {
	resp, err := c.api.ListNewsWithResponse(ctx, &apinews.ListNewsParams{Lang: apinews.Lang(lang), Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("apinewsclient: ListNews: %w", err)
	}
	if resp.JSON200 != nil {
		return resp.JSON200, nil
	}
	return nil, toStatusError("ListNews", resp.StatusCode())
}

// GetNewsDetail は指定言語でニュース記事詳細を返す。articleID は ULID。
func (c *Client) GetNewsDetail(ctx context.Context, articleID string, lang string) (*apinews.NewsDetail, error) {
	resp, err := c.api.GetNewsDetailWithResponse(ctx, articleID, &apinews.GetNewsDetailParams{Lang: apinews.Lang(lang)})
	if err != nil {
		return nil, fmt.Errorf("apinewsclient: GetNewsDetail: %w", err)
	}
	if resp.JSON200 != nil {
		return resp.JSON200, nil
	}
	return nil, toStatusError("GetNewsDetail", resp.StatusCode())
}

// toStatusError は HTTP status code を sentinel error (errors.Is 分岐可能) に変換する。
func toStatusError(op string, code int) error {
	var sentinel error
	switch {
	case code == http.StatusUnauthorized:
		sentinel = ErrUnauthorized
	case code == http.StatusNotFound:
		sentinel = ErrNotFound
	case code == http.StatusBadRequest:
		sentinel = ErrBadRequest
	case code >= http.StatusInternalServerError:
		sentinel = ErrInternalServer
	default:
		return fmt.Errorf("apinewsclient: %s: unexpected status %d", op, code)
	}
	return fmt.Errorf("apinewsclient: %s: %w", op, sentinel)
}
