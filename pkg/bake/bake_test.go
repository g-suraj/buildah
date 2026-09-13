package bake

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	buildxbake "github.com/docker/buildx/bake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func definitionFile(t *testing.T, name, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	return path
}

func TestResolveAndTranslate(t *testing.T) {
	t.Setenv("VERSION", "environment")
	t.Setenv("UNSET", "must-not-leak")
	file := definitionFile(t, "docker-bake.hcl", `
variable "VERSION" { default = "default" }
group "default" { targets = ["worker", "api", "api"] }
target "base" {
  context = "project"
  dockerfile = "Dockerfile.custom"
  args = { MODE = "debug", EMPTY = "", UNSET = null }
  labels = { "example.version" = VERSION }
}
target "api" {
  inherits = ["base"]
  tags = ["localhost/api:${VERSION}", "localhost/api:latest"]
  target = "release"
  no-cache = true
  pull = true
}
target "worker" {
  inherits = ["base"]
  tags = ["localhost/worker:${VERSION}"]
}
`)
	d, err := Resolve(context.Background(), []string{file}, nil, []string{"api.args.MODE=optimized,with=equals"}, nil)
	require.NoError(t, err)
	builds, err := d.Builds()
	require.NoError(t, err)
	require.Len(t, builds, 2)
	assert.Equal(t, "api", builds[0].Name)
	assert.Equal(t, "worker", builds[1].Name)
	contextDir, err := filepath.Abs("project")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"--file=" + filepath.Join(contextDir, "Dockerfile.custom"),
		"--build-arg=EMPTY=", "--build-arg=MODE=optimized,with=equals",
		"--label=example.version=environment", "--tag=localhost/api:environment",
		"--tag=localhost/api:latest", "--target=release", "--no-cache=true", "--pull=always", contextDir,
	}, builds[0].Arguments)
	assert.Contains(t, builds[1].Arguments, "--build-arg=MODE=debug")
	assert.NotContains(t, builds[1].Arguments, "--no-cache=true")
	assert.NotContains(t, builds[1].Arguments, "--build-arg=UNSET=must-not-leak")
}

func TestFileMergingAndSelection(t *testing.T) {
	file := definitionFile(t, "docker-bake.hcl", `
group "default" { targets = ["api", "worker"] }
target "api" { args = { MODE = "debug" } }
target "worker" {}
`)
	override := definitionFile(t, "docker-bake.override.json", `{"target":{"api":{"args":{"MODE":"file"}}}}`)
	d, err := Resolve(context.Background(), []string{file, override}, []string{"api"}, []string{"api.args.MODE=cli"}, nil)
	require.NoError(t, err)
	builds, err := d.Builds()
	require.NoError(t, err)
	require.Len(t, builds, 1)
	assert.Contains(t, builds[0].Arguments, "--build-arg=MODE=cli")
}

func TestComposeAndMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		count      int
	}{
		{"compose.yaml", "services:\n  api:\n    image: localhost/api:test\n    build:\n      context: .\n      args:\n        MODE: release\n", 1},
		{"docker-bake.hcl", `target "default" {
  name = "app-${version}"
  matrix = { version = ["one", "two"] }
  tags = ["localhost/app:${version}"]
}`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Resolve(context.Background(), []string{definitionFile(t, tc.name, tc.data)}, nil, nil, nil)
			require.NoError(t, err)
			builds, err := d.Builds()
			require.NoError(t, err)
			assert.Len(t, builds, tc.count)
		})
	}
}

func TestPrintPreservesUnsupportedSettings(t *testing.T) {
	d, err := Resolve(context.Background(), []string{"-"}, nil, nil, strings.NewReader(`target "default" { platforms = ["linux/arm64"] }`))
	require.NoError(t, err)
	data, err := json.Marshal(d)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"platforms":["linux/arm64"]`)
	builds, err := d.Builds()
	assert.Nil(t, builds)
	assert.ErrorContains(t, err, `bake target "default": unsupported setting "platforms"`)
}

func TestRejectUnsupportedBeforeReturningPlan(t *testing.T) {
	for _, field := range []string{
		`platforms = ["linux/arm64"]`, `output = ["type=local,dest=out"]`,
		`cache-to = ["type=registry,ref=localhost/cache"]`, `dockerfile-inline = "FROM scratch"`,
		`network = "host"`, `entitlements = ["network.host"]`, `secret = ["id=key,src=key"]`,
		`context = "https://example.com/context.git"`, `dockerfile = "https://example.com/Dockerfile"`,
		`contexts = { base = "target:a" }`,
	} {
		t.Run(field, func(t *testing.T) {
			file := definitionFile(t, "docker-bake.hcl", "group \"default\" { targets = [\"a\", \"z\"] }\ntarget \"a\" {}\ntarget \"z\" { "+field+" }")
			d, err := Resolve(context.Background(), []string{file}, nil, nil, nil)
			require.NoError(t, err)
			builds, err := d.Builds()
			assert.Nil(t, builds)
			assert.ErrorContains(t, err, `bake target "z"`)
		})
	}
}

func TestResolutionErrors(t *testing.T) {
	file := definitionFile(t, "docker-bake.hcl", `target "default" {}`)
	_, err := Resolve(context.Background(), []string{file}, []string{"missing"}, nil, nil)
	assert.ErrorContains(t, err, "missing")
	_, err = Resolve(context.Background(), []string{file}, nil, []string{"invalid"}, nil)
	assert.Error(t, err)
	_, err = Resolve(context.Background(), []string{"-"}, nil, nil, nil)
	assert.ErrorContains(t, err, "stdin is required")
	_, err = Resolve(context.Background(), []string{"-"}, nil, nil, strings.NewReader("not valid {"))
	assert.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Resolve(ctx, []string{file}, nil, nil, nil)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestDiscovery(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := Resolve(context.Background(), nil, nil, nil, nil)
	assert.ErrorContains(t, err, "couldn't find a bake definition")
	require.NoError(t, os.WriteFile("docker-bake.hcl", []byte(`target "default" { tags = ["localhost/app:base"] }`), 0o600))
	require.NoError(t, os.WriteFile("docker-bake.override.hcl", []byte(`target "default" { tags = ["localhost/app:override"] }`), 0o600))
	d, err := Resolve(context.Background(), nil, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"localhost/app:override"}, d.Targets["default"].Tags)
}

func TestFalseAndNullValues(t *testing.T) {
	no := false
	d := Definition{Targets: map[string]*buildxbake.Target{"default": {NoCache: &no, Pull: &no}}}
	builds, err := d.Builds()
	require.NoError(t, err)
	assert.Contains(t, builds[0].Arguments, "--no-cache=false")
	assert.Contains(t, builds[0].Arguments, "--pull=missing")
}
