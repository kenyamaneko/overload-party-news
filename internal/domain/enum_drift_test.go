package domain_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
)

type openapiSchemas struct {
	Components struct {
		Schemas struct {
			Lang struct {
				Enum []string `yaml:"enum"`
			} `yaml:"Lang"`
			Source struct {
				Enum []string `yaml:"enum"`
			} `yaml:"Source"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

func readOpenAPISchemas(t *testing.T) openapiSchemas {
	t.Helper()
	data, err := os.ReadFile("../../data/openapi.yaml")
	require.NoError(t, err)

	var doc openapiSchemas
	require.NoError(t, yaml.Unmarshal(data, &doc))
	return doc
}

func TestSupportedEnumsMatchOpenAPIContract(t *testing.T) {
	t.Run("[ニュースドメインモデル]対応言語コード・ソース種別の集合とdata/openapi.yamlの整合", func(t *testing.T) {
		t.Run("対応言語コードの集合が、data/openapi.yamlのLang enumの値集合と一致する", func(t *testing.T) {
			doc := readOpenAPISchemas(t)

			assert.ElementsMatch(t, doc.Components.Schemas.Lang.Enum, domain.SupportedLangs)
		})

		t.Run("ソース種別の集合が、data/openapi.yamlのSource enumの値集合と一致する", func(t *testing.T) {
			doc := readOpenAPISchemas(t)

			assert.ElementsMatch(t, doc.Components.Schemas.Source.Enum, domain.Sources)
		})
	})
}
