package appconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAppPreservesCommandArguments(t *testing.T) {
	doc, err := Parse([]byte("apiVersion: dzjjy/v1\nkind: App\nmetadata:\n  name: demo\nspec:\n  artifact: ./app.zip\n  runtime:\n    type: python\n    version: \"3.12\"\n  command: [python, app.py, --message, \"hello world\"]\n  sandbox:\n    type: docker\n  lifecycle:\n    ttl: 30m\n"))
	require.NoError(t, err)
	require.Equal(t, []string{"python", "app.py", "--message", "hello world"}, doc.Spec.Command)
}

func TestParseStaticSite(t *testing.T) {
	doc, err := Parse([]byte("apiVersion: dzjjy/v1\nkind: StaticSite\nmetadata:\n  name: docs\nspec:\n  artifact: ./site.zip\n  port: 0\n"))
	require.NoError(t, err)
	require.Equal(t, "process", doc.Spec.Sandbox.Type)
	require.NotNil(t, doc.Spec.Port)
}

func TestParseRejectsUnknownFieldsAndInvalidRuntime(t *testing.T) {
	base := "apiVersion: dzjjy/v1\nkind: App\nmetadata:\n  name: demo\nspec:\n  artifact: ./app.zip\n  command: [python, app.py]\n"
	_, err := Parse([]byte(base + "  commandx: ignored\n"))
	require.Error(t, err)

	_, err = Parse([]byte("apiVersion: dzjjy/v1\nkind: App\nmetadata:\n  name: demo\nspec:\n  artifact: ./app.zip\n  runtime: {type: python, version: latest}\n  command: [python, app.py]\n"))
	require.ErrorContains(t, err, "unsupported runtime template")
}

func TestParseRejectsZeroTTLAndMultipleDocuments(t *testing.T) {
	_, err := Parse([]byte("apiVersion: dzjjy/v1\nkind: StaticSite\nmetadata: {name: docs}\nspec:\n  artifact: site.zip\n  lifecycle: {ttl: 0s}\n"))
	require.ErrorContains(t, err, "positive duration")
	_, err = Parse([]byte("apiVersion: dzjjy/v1\nkind: StaticSite\nmetadata: {name: docs}\nspec: {artifact: site.zip}\n---\nkind: App\n"))
	require.ErrorContains(t, err, "multiple YAML documents")
}
