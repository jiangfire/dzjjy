package appconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAppAllowsCommandWithoutArtifact(t *testing.T) {
	doc, err := Parse([]byte("apiVersion: dzjjy/v1\nkind: App\nmetadata: {name: native}\nspec:\n  command: [./server, --path, \"C:/Program Files/app\"]\n"))
	require.NoError(t, err)
	require.Empty(t, doc.Spec.Artifact)
	require.Equal(t, "C:/Program Files/app", doc.Spec.Command[2])
}

func TestParseStaticSiteRequiresArtifact(t *testing.T) {
	_, err := Parse([]byte("apiVersion: dzjjy/v1\nkind: StaticSite\nmetadata: {name: docs}\nspec: {}\n"))
	require.ErrorContains(t, err, "spec.artifact is required")
}
