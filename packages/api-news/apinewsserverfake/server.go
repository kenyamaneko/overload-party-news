// Package apinewsserverfake は news の REST 契約を実装する httptest.Server ラッパー。
// 各 endpoint は Fn field (func callback) で応答を制御し、Fn=nil なら既定値を返す。
package apinewsserverfake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"

	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

// Server は news REST 契約を実装する httptest.Server wrapper。
type Server struct {
	mu  sync.Mutex
	srv *httptest.Server

	// GetHealthFn は GET /health の応答を決定する (nil は 200 + status:"ok")。
	GetHealthFn func() (int, any)

	// ListNewsFn は GET /api/v1/news?lang=&limit= の応答を決定する (nil は 200 + 空配列)。
	ListNewsFn func(lang string, limit int) (int, any)

	// GetNewsDetailFn は GET /api/v1/news/{articleID}?lang= の応答を決定する (nil は 404)。
	GetNewsDetailFn func(articleID string, lang string) (int, any)
}

// NewServer は起動済み Server を返す。テスト終了時に Close() すること。
func NewServer() *Server {
	s := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleGetHealth)
	mux.HandleFunc("GET /api/v1/news", s.handleListNews)
	mux.HandleFunc("GET /api/v1/news/{articleID}", s.handleGetNewsDetail)
	s.srv = httptest.NewServer(mux)
	return s
}

// URL は httptest.Server のベース URL を返す。
func (s *Server) URL() string { return s.srv.URL }

// Close は内部 httptest.Server を閉じる。
func (s *Server) Close() { s.srv.Close() }

func (s *Server) handleGetHealth(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	fn := s.GetHealthFn
	s.mu.Unlock()
	if fn == nil {
		writeJSON(w, http.StatusOK, apinews.HealthResponse{Status: "ok"})
		return
	}
	status, body := fn()
	writeJSON(w, status, body)
}

func (s *Server) handleListNews(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	fn := s.ListNewsFn
	s.mu.Unlock()

	lang := r.URL.Query().Get("lang")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if fn == nil {
		writeJSON(w, http.StatusOK, apinews.NewsListResponse{Articles: []apinews.NewsListItem{}})
		return
	}
	status, body := fn(lang, limit)
	writeJSON(w, status, body)
}

func (s *Server) handleGetNewsDetail(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	fn := s.GetNewsDetailFn
	s.mu.Unlock()

	articleID := r.PathValue("articleID")
	lang := r.URL.Query().Get("lang")
	if fn == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "article not found"})
		return
	}
	status, body := fn(articleID, lang)
	writeJSON(w, status, body)
}

// writeJSON は status code を書き、body が非 nil なら JSON encode して送る。
// body が nil の場合は body 無しでレスポンスを終わる (4xx/5xx のエラー body は SDK 側で raw 扱い)。
func writeJSON(w http.ResponseWriter, status int, body any) {
	if body == nil {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
