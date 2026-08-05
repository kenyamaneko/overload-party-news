package domain_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/kenyamaneko/overload-party-news/internal/domain"
)

func TestEnumDriftAgainstOpenAPISpec(t *testing.T) {
	t.Run("domainとopenapi.yamlのenum整合", func(t *testing.T) {
		// SSoT は domain 側。openapi.yaml は外部公開ドキュメントとして同じ値集合を持つ必要がある。
		spec := loadOpenAPISpec(t)

		cases := []struct {
			name       string
			schemaName string
			want       []string
		}{
			{
				name:       "Lang enumがdomainとopenapi.yamlで一致する",
				schemaName: "Lang",
				want:       domain.SupportedLangs,
			},
			{
				name:       "Source enumがdomainとopenapi.yamlで一致する",
				schemaName: "Source",
				want:       domain.Sources,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got := specEnumValues(t, spec, tc.schemaName)
				require.ElementsMatch(t, tc.want, got, "domain と openapi.yaml の %s enum が drift している", tc.schemaName)
			})
		}
	})
}

// loadOpenAPISpec は data/openapi.yaml をパースして返す。
func loadOpenAPISpec(t *testing.T) map[string]interface{} {
	t.Helper()
	specPath := filepath.Join(repoRoot(t), "data", "openapi.yaml")
	raw, err := os.ReadFile(specPath)
	require.NoError(t, err)
	var doc map[string]interface{}
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	return doc
}

// specEnumValues は components/schemas/<name>/enum 配下の値一覧を取り出す。
func specEnumValues(t *testing.T, spec map[string]interface{}, schemaName string) []string {
	t.Helper()
	components, ok := spec["components"].(map[string]interface{})
	require.True(t, ok, "components が見つからない")
	schemas, ok := components["schemas"].(map[string]interface{})
	require.True(t, ok, "components/schemas が見つからない")
	schema, ok := schemas[schemaName].(map[string]interface{})
	require.True(t, ok, "components/schemas/%s が見つからない", schemaName)
	rawEnum, ok := schema["enum"].([]interface{})
	require.True(t, ok, "components/schemas/%s/enum が無い、または配列でない", schemaName)
	out := make([]string, 0, len(rawEnum))
	for _, v := range rawEnum {
		s, ok := v.(string)
		require.True(t, ok, "%s の enum 値が文字列でない", schemaName)
		out = append(out, s)
	}
	return out
}

// repoRoot は本ファイルから見たリポジトリルートを返す (internal/domain/ から 2 階層上)。
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Join(wd, "..", "..")
}
