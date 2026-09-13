package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/buildah/pkg/bake"
)

func TestBakePrint(t *testing.T) {
	command := newBakeCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetIn(strings.NewReader(`target "default" { platforms = ["linux/arm64"] }`))
	command.SetArgs([]string{"--file=-", "--print"})
	require.NoError(t, command.Execute())
	assert.Contains(t, output.String(), `"linux/arm64"`)
}

func TestPrepareBakeBuilds(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "context,with\"quotes")
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o600))
	d, err := bake.Resolve(context.Background(), []string{"-"}, nil, nil, strings.NewReader(`
group "default" { targets = ["api", "worker"] }
target "api" {
  args = { MODE = "release", EMPTY = "", UNSET = null }
  labels = { role = "api" }
  tags = ["localhost/api:test", "localhost/api:latest"]
  no-cache = true
}
target "worker" { tags = ["localhost/worker:test"] }
`))
	require.NoError(t, err)
	for _, target := range d.Targets {
		target.Context = &dir
	}
	builds, err := d.Builds()
	require.NoError(t, err)
	prepared, cleanup, err := prepareBakeBuilds(builds)
	defer cleanup()
	require.NoError(t, err)
	require.Len(t, prepared, 2)
	assert.Equal(t, map[string]string{"MODE": "release", "EMPTY": ""}, prepared[0].options.Args)
	assert.Equal(t, "localhost/api:test", prepared[0].options.Output)
	assert.Equal(t, []string{"localhost/api:latest"}, prepared[0].options.AdditionalTags)
	assert.Contains(t, prepared[0].options.Labels, "role=api")
	assert.True(t, prepared[0].options.NoCache)
	assert.Empty(t, prepared[1].options.Args)
	assert.False(t, prepared[1].options.NoCache)
	assert.Equal(t, []string{filepath.Join(dir, "Dockerfile")}, prepared[0].containerfiles)
	canonicalDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, canonicalDir, prepared[0].options.ContextDirectory)
	builds[1].Arguments[0] = "--file=" + filepath.Join(t.TempDir(), "missing")
	prepared, cleanup2, err := prepareBakeBuilds(builds)
	defer cleanup2()
	assert.Nil(t, prepared)
	assert.ErrorContains(t, err, `bake target "worker"`)
}
