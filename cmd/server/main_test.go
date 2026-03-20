package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/internal/server/handler"
)

func TestCreateMultiAppHandler_DeleteWithoutActionRoutesToRemove(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "server-main-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	uploadDir := filepath.Join(tmpDir, "uploads")
	workDir := filepath.Join(tmpDir, "workspace")
	logDir := filepath.Join(tmpDir, "logs")
	require.NoError(t, os.MkdirAll(uploadDir, 0755))
	require.NoError(t, os.MkdirAll(workDir, 0755))
	require.NoError(t, os.MkdirAll(logDir, 0755))

	h := handler.NewHandler(uploadDir, workDir, logDir)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/apps/demo", nil)
	rec := httptest.NewRecorder()

	createMultiAppHandler(h)(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "application removed")
}
