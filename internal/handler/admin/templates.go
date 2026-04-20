package admin

import (
	"embed"
	"fmt"
	"html/template"

	apinews "github.com/kenyamaneko/overload-party-news/packages/api-news"
)

//go:embed templates/*.html templates/partials/*.html
var templatesFS embed.FS

// templates は管理 UI が描画する 3 系統のテンプレートセット。
// 各エントリは (layout + partial + 固有ページ) を一度にパースし、
// ExecuteTemplate の呼び出しだけでページ全体を描画できる。
type templates struct {
	list *template.Template // layout + list + partials/row
	edit *template.Template // layout + edit + partials/row
	row  *template.Template // partials/row 単体 (publish/reject の部分差し替え用)
}

// parseTemplates は embed.FS からテンプレート集合を組み立てる。起動時に 1 回だけ呼ぶ。
func parseTemplates() (*templates, error) {
	base := template.New("").Funcs(templateFuncMap())

	list, err := base.Clone()
	if err != nil {
		return nil, fmt.Errorf("clone base for list: %w", err)
	}
	list, err = list.ParseFS(templatesFS, "templates/layout.html", "templates/list.html", "templates/partials/row.html")
	if err != nil {
		return nil, fmt.Errorf("parse list templates: %w", err)
	}

	edit, err := base.Clone()
	if err != nil {
		return nil, fmt.Errorf("clone base for edit: %w", err)
	}
	edit, err = edit.ParseFS(templatesFS, "templates/layout.html", "templates/edit.html", "templates/partials/row.html")
	if err != nil {
		return nil, fmt.Errorf("parse edit templates: %w", err)
	}

	row, err := base.Clone()
	if err != nil {
		return nil, fmt.Errorf("clone base for row: %w", err)
	}
	row, err = row.ParseFS(templatesFS, "templates/partials/row.html")
	if err != nil {
		return nil, fmt.Errorf("parse row template: %w", err)
	}

	return &templates{list: list, edit: edit, row: row}, nil
}

// templateFuncMap は Go テンプレートから呼べるヘルパー関数を集める。
func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		// statusNE は typed な apinews.Status と生文字列を比較するためのヘルパー。
		"statusNE": func(s apinews.Status, target string) bool {
			return string(s) != target
		},
		// findTranslation は指定 lang の翻訳を検索して返す。存在しなければ nil。
		// 編集画面で各言語タブの既存内容を埋めるために使う。
		"findTranslation": func(translations []apinews.Translation, lang string) *apinews.Translation {
			for i := range translations {
				if translations[i].Lang == lang {
					return &translations[i]
				}
			}
			return nil
		},
	}
}
