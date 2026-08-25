package ingest_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
	"github.com/kenyamaneko/overload-party-news/internal/port"
	"github.com/kenyamaneko/overload-party-news/internal/usecase/ingest"
	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

func validCollectedEvent() apinews.ArticleCollectedEvent {
	return apinews.ArticleCollectedEvent{
		ArticleID: "article-id-valid-001",
		Source:    "aws",
		SourceURL: "https://example.com/articles/1",
		Tags:      []string{"cloud", "release"},
		Translations: []apinews.EventTranslation{
			{Lang: "ja", Title: "タイトル", Summary: "要約", Body: "本文"},
		},
	}
}

type recordedInsert struct {
	article domain.Article
	lang    string
	title   string
	summary string
	body    string
}

func newRecordingWriter(inserted bool, err error) (*port.MockNewsRepo, *[]recordedInsert) {
	var calls []recordedInsert
	writer := &port.MockNewsRepo{
		InsertArticleWithTranslationFn: func(_ context.Context, article domain.Article, lang, title, summary, body string) (bool, error) {
			calls = append(calls, recordedInsert{article: article, lang: lang, title: title, summary: summary, body: body})
			return inserted, err
		},
	}
	return writer, &calls
}

func TestIngestInteractorInsert(t *testing.T) {
	t.Run("記事収集イベントの取込可否判定", func(t *testing.T) {
		t.Run("article_id・source・source_url・翻訳(ja1件、title・summary・body全て)が揃った完全なイベントのとき、記事と翻訳が書き込み先に渡され、渡された内容がイベントの値と一致する", func(t *testing.T) {
			event := validCollectedEvent()
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.NoError(t, err)
			require.Len(t, *calls, 1)
			got := (*calls)[0]
			assert.Equal(t, event.ArticleID, got.article.ArticleID)
			assert.Equal(t, event.SourceURL, got.article.SourceURL)
			assert.Equal(t, event.Translations[0].Lang, got.lang)
			assert.Equal(t, event.Translations[0].Title, got.title)
			assert.Equal(t, event.Translations[0].Summary, got.summary)
			assert.Equal(t, event.Translations[0].Body, got.body)
		})

		t.Run("source_published_atが無いイベントでも、記事と翻訳が書き込み先に渡される", func(t *testing.T) {
			event := validCollectedEvent()
			event.SourcePublishedAt = nil
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.NoError(t, err)
			require.Len(t, *calls, 1)
			assert.Nil(t, (*calls)[0].article.SourcePublishedAt)
		})

		t.Run("tagsが無いイベントでも、記事と翻訳が書き込み先に渡される", func(t *testing.T) {
			event := validCollectedEvent()
			event.Tags = nil
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.NoError(t, err)
			require.Len(t, *calls, 1)
			assert.Empty(t, (*calls)[0].article.Tags)
		})

		t.Run("article_idがちょうど26文字のとき、記事と翻訳が書き込み先に渡される", func(t *testing.T) {
			event := validCollectedEvent()
			event.ArticleID = strings.Repeat("a", 26)
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.NoError(t, err)
			require.Len(t, *calls, 1)
			assert.Equal(t, event.ArticleID, (*calls)[0].article.ArticleID)
		})

		t.Run("sourceがちょうど20文字のとき、記事と翻訳が書き込み先に渡される", func(t *testing.T) {
			event := validCollectedEvent()
			event.Source = strings.Repeat("s", 20)
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.NoError(t, err)
			require.Len(t, *calls, 1)
			assert.Equal(t, event.Source, (*calls)[0].article.Source)
		})

		t.Run("書き込みが新規に行われたとき、取込結果は新規に取り込んだことを表す値になる", func(t *testing.T) {
			event := validCollectedEvent()
			writer, _ := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			inserted, err := uc.Insert(context.Background(), event)

			require.NoError(t, err)
			assert.True(t, inserted)
		})

		t.Run("記事が既に取り込み済みで書き込みが行われなかったとき、取込結果は新規に取り込んでいないことを表す値になる", func(t *testing.T) {
			event := validCollectedEvent()
			writer, _ := newRecordingWriter(false, nil)
			uc := ingest.New(writer)

			inserted, err := uc.Insert(context.Background(), event)

			require.NoError(t, err)
			assert.False(t, inserted)
		})

		t.Run("書き込み先がエラーを返すとき、そのエラーが取込結果として伝わる", func(t *testing.T) {
			event := validCollectedEvent()
			writerErr := errors.New("insert failed")
			writer, _ := newRecordingWriter(false, writerErr)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			assert.ErrorIs(t, err, writerErr)
		})

		t.Run("article_idが無いイベントは、書き込み先に何も渡されずに拒否される", func(t *testing.T) {
			event := validCollectedEvent()
			event.ArticleID = ""
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.Error(t, err)
			assert.Empty(t, *calls)
		})

		t.Run("article_idが27文字(上限超過)のイベントは、書き込み先に何も渡されずに拒否される", func(t *testing.T) {
			event := validCollectedEvent()
			event.ArticleID = strings.Repeat("a", 27)
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.Error(t, err)
			assert.Empty(t, *calls)
		})

		t.Run("sourceが無いイベントは、書き込み先に何も渡されずに拒否される", func(t *testing.T) {
			event := validCollectedEvent()
			event.Source = ""
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.Error(t, err)
			assert.Empty(t, *calls)
		})

		t.Run("sourceが21文字(上限超過)のイベントは、書き込み先に何も渡されずに拒否される", func(t *testing.T) {
			event := validCollectedEvent()
			event.Source = strings.Repeat("s", 21)
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.Error(t, err)
			assert.Empty(t, *calls)
		})

		t.Run("source_urlが無いイベントは、書き込み先に何も渡されずに拒否される", func(t *testing.T) {
			event := validCollectedEvent()
			event.SourceURL = ""
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.Error(t, err)
			assert.Empty(t, *calls)
		})

		t.Run("翻訳が0件のイベントは、書き込み先に何も渡されずに拒否される", func(t *testing.T) {
			event := validCollectedEvent()
			event.Translations = nil
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.Error(t, err)
			assert.Empty(t, *calls)
		})

		t.Run("翻訳がjaとenの2件あるイベントは、書き込み先に何も渡されずに拒否される", func(t *testing.T) {
			event := validCollectedEvent()
			event.Translations = []apinews.EventTranslation{
				{Lang: "ja", Title: "タイトル", Summary: "要約", Body: "本文"},
				{Lang: "en", Title: "title", Summary: "summary", Body: "body"},
			}
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.Error(t, err)
			assert.Empty(t, *calls)
		})

		t.Run("翻訳の言語がja以外(en)1件だけのイベントは、書き込み先に何も渡されずに拒否される", func(t *testing.T) {
			event := validCollectedEvent()
			event.Translations = []apinews.EventTranslation{
				{Lang: "en", Title: "title", Summary: "summary", Body: "body"},
			}
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.Error(t, err)
			assert.Empty(t, *calls)
		})

		t.Run("翻訳の言語が無いイベントは、書き込み先に何も渡されずに拒否される", func(t *testing.T) {
			event := validCollectedEvent()
			event.Translations = []apinews.EventTranslation{
				{Lang: "", Title: "タイトル", Summary: "要約", Body: "本文"},
			}
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.Error(t, err)
			assert.Empty(t, *calls)
		})

		t.Run("翻訳のtitleが無いイベントは、書き込み先に何も渡されずに拒否される", func(t *testing.T) {
			event := validCollectedEvent()
			event.Translations = []apinews.EventTranslation{
				{Lang: "ja", Title: "", Summary: "要約", Body: "本文"},
			}
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.Error(t, err)
			assert.Empty(t, *calls)
		})

		t.Run("翻訳のsummaryが無いイベントは、書き込み先に何も渡されずに拒否される", func(t *testing.T) {
			event := validCollectedEvent()
			event.Translations = []apinews.EventTranslation{
				{Lang: "ja", Title: "タイトル", Summary: "", Body: "本文"},
			}
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.Error(t, err)
			assert.Empty(t, *calls)
		})

		t.Run("翻訳のbodyが無いイベントは、書き込み先に何も渡されずに拒否される", func(t *testing.T) {
			event := validCollectedEvent()
			event.Translations = []apinews.EventTranslation{
				{Lang: "ja", Title: "タイトル", Summary: "要約", Body: ""},
			}
			writer, calls := newRecordingWriter(true, nil)
			uc := ingest.New(writer)

			_, err := uc.Insert(context.Background(), event)

			require.Error(t, err)
			assert.Empty(t, *calls)
		})
	})
}
