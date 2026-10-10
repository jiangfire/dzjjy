package k8simport_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/internal/k8simport"
	"github.com/jiangfire/dzjjy/internal/server/appconfig"
)

const deploymentYAML = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 3
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: python:3.12-slim
          command: ["python"]
          args: ["app.py", "--port", "8000"]
          ports:
            - name: http
              containerPort: 8000
          env:
            - name: LOG_LEVEL
              value: info
            - name: API_KEY
              valueFrom:
                secretKeyRef:
                  name: web-secrets
                  key: api-key
          resources:
            limits:
              memory: 256Mi
              cpu: 500m
      volumes:
        - name: cache
          emptyDir: {}
`

const serviceYAML = `
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  type: ClusterIP
  ports:
    - name: http
      port: 80
      targetPort: 8000
`

func convert(t *testing.T, manifest string) k8simport.Document {
	t.Helper()
	manifests, err := k8simport.Parse([]byte(manifest))
	require.NoError(t, err)
	document, err := k8simport.Convert(manifests)
	require.NoError(t, err)
	return document
}

func TestConvertDeploymentProducesValidDzjjyManifest(t *testing.T) {
	document := convert(t, deploymentYAML+"\n---\n"+serviceYAML)

	// The generated manifest must satisfy dzjjy's own strict parser.
	parsed, err := appconfig.Parse([]byte(document.YAML))
	require.NoError(t, err)
	assert.Equal(t, "dzjjy/v1", parsed.APIVersion)
	assert.Equal(t, "App", parsed.Kind)
	assert.Equal(t, "web", parsed.Metadata.Name)
	assert.Equal(t, "docker", parsed.Spec.Sandbox.Type)
	require.NotNil(t, parsed.Spec.Runtime)
	assert.Equal(t, "python", parsed.Spec.Runtime.Type)
	assert.Equal(t, "3.12", parsed.Spec.Runtime.Version)
	assert.Equal(t, []string{"python", "app.py", "--port", "8000"}, parsed.Spec.Command)
	require.NotNil(t, parsed.Spec.Resources.Memory)
	assert.Equal(t, "256m", parsed.Spec.Resources.Memory)
	assert.Equal(t, "0.5", parsed.Spec.Resources.CPUs)
	require.Len(t, parsed.Spec.Env, 1)
	assert.Equal(t, "LOG_LEVEL", parsed.Spec.Env[0].Name)
	require.Len(t, parsed.Spec.Secrets, 1)
	assert.Equal(t, "web-secrets/api-key", parsed.Spec.Secrets[0].Key)
	assert.Equal(t, "API_KEY", parsed.Spec.Secrets[0].Env)
	require.NotNil(t, parsed.Spec.Network)
	require.Len(t, parsed.Spec.Network.Ports, 1)
	assert.Equal(t, 8000, parsed.Spec.Network.Ports[0].Container)
}

func TestConvertRecordsUnsupportedFeaturesAsNotes(t *testing.T) {
	document := convert(t, deploymentYAML+"\n---\n"+serviceYAML)
	joined := strings.Join(document.Notes, "\n")
	assert.Contains(t, joined, "replicas=3")
	assert.Contains(t, joined, "secret(s) imported")
}

func TestConvertRejectsUnsupportedImage(t *testing.T) {
	manifest := strings.Replace(deploymentYAML, "python:3.12-slim", "ubuntu:24.04", 1)
	manifests, err := k8simport.Parse([]byte(manifest))
	require.NoError(t, err)
	_, err = k8simport.Convert(manifests)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a dzjjy maintained runtime template")
}

func TestConvertRejectsMultiContainer(t *testing.T) {
	manifest := strings.Replace(deploymentYAML,
		"      volumes:",
		"        - name: sidecar\n          image: python:3.12-slim\n          command: [\"python\"]\n      volumes:", 1)
	manifests, err := k8simport.Parse([]byte(manifest))
	require.NoError(t, err)
	_, err = k8simport.Convert(manifests)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one container")
}

func TestConvertRejectsUnsupportedKind(t *testing.T) {
	manifests, err := k8simport.Parse([]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"))
	require.NoError(t, err)
	_, err = k8simport.Convert(manifests)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported Kubernetes kind")
}

func TestConvertServiceNodePortBecomesHostPort(t *testing.T) {
	service := strings.Replace(serviceYAML, "targetPort: 8000", "targetPort: 8000\n      nodePort: 31000", 1)
	document := convert(t, deploymentYAML+"\n---\n"+service)
	parsed, err := appconfig.Parse([]byte(document.YAML))
	require.NoError(t, err)
	require.Len(t, parsed.Spec.Network.Ports, 1)
	assert.Equal(t, 31000, parsed.Spec.Network.Ports[0].Host)
	assert.Equal(t, 8000, parsed.Spec.Network.Ports[0].Container)
}

func TestConvertNamedTargetPortResolvesAgainstContainer(t *testing.T) {
	service := strings.Replace(serviceYAML, "targetPort: 8000", "targetPort: http", 1)
	document := convert(t, deploymentYAML+"\n---\n"+service)
	parsed, err := appconfig.Parse([]byte(document.YAML))
	require.NoError(t, err)
	require.Len(t, parsed.Spec.Network.Ports, 1)
	assert.Equal(t, 8000, parsed.Spec.Network.Ports[0].Container)
}

func TestConvertImportsEmptyDirAsNamedVolume(t *testing.T) {
	manifest := strings.Replace(deploymentYAML,
		"          resources:",
		"          volumeMounts:\n            - name: cache\n              mountPath: /cache\n          resources:", 1)
	document := convert(t, manifest)
	parsed, err := appconfig.Parse([]byte(document.YAML))
	require.NoError(t, err)
	require.Len(t, parsed.Spec.Volumes, 1)
	assert.Equal(t, "/cache", parsed.Spec.Volumes[0].Path)
	assert.Equal(t, "dzjjy-web-cache", parsed.Spec.Volumes[0].Name)
}

func TestConvertRequiresExplicitCommand(t *testing.T) {
	manifest := strings.Replace(deploymentYAML, `          command: ["python"]`+"\n"+`          args: ["app.py", "--port", "8000"]`+"\n", "", 1)
	manifests, err := k8simport.Parse([]byte(manifest))
	require.NoError(t, err)
	_, err = k8simport.Convert(manifests)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "explicit command")
}

func TestParseRejectsEmptyManifest(t *testing.T) {
	_, err := k8simport.Parse([]byte("# nothing here\n"))
	require.Error(t, err)
}

func TestParseFlattensListKind(t *testing.T) {
	list := "apiVersion: v1\nkind: List\nitems:\n  - " + strings.ReplaceAll(deploymentYAML, "\n", "\n    ")
	manifests, err := k8simport.Parse([]byte(list))
	require.NoError(t, err)
	require.Len(t, manifests, 1)
	assert.Equal(t, "Deployment", manifests[0].Kind)
}
