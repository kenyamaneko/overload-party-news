package admin

import (
	"embed"
	"fmt"
	"html/template"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
)

//go:embed templates/*.html templates/partials/*.html
var templatesFS embed.FS

// templates は管理 UI が描画する 3 系統のテンプレートセット。
type templates struct {
	list *template.Template // layout + list + partials/row
	edit *template.Template // layout + edit + partials/row
	row  *template.Template // partials/row 単体 (publish/reject の部分差し替え用)
}

// parseTemplates は embed.FS からテンプレート集合を組み立てる。起動時に 1 回だけ呼ぶ。
func parseTemplates() (*templates, error) {
	base := template.New("").Funcs(buildTemplateFuncMap())

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

// buildTemplateFuncMap は Go テンプレートから呼べるヘルパー関数を集める。
func buildTemplateFuncMap() template.FuncMap {
	return template.FuncMap{
		"statusNE": func(s domain.Status, target string) bool {
			return string(s) != target
		},
		"findTranslation": func(translations []domain.Translation, lang string) *domain.Translation {
			for i := range translations {
				if translations[i].Lang == lang {
					return &translations[i]
				}
			}
			return nil
		},
	}
}
