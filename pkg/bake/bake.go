// Package bake resolves Docker Bake definitions using Buildx and translates the
// supported targets into build arguments shared by Buildah's build frontend.
package bake

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/containerd/platforms"
	buildxbake "github.com/docker/buildx/bake"
)

// Definition is a resolved Bake configuration. Its JSON representation matches
// the group/target structure printed by docker buildx bake --print.
type Definition struct {
	Groups  map[string]*buildxbake.Group  `json:"group,omitempty"`
	Targets map[string]*buildxbake.Target `json:"target"`
}

// Resolve reads local files (or stdin for "-") and resolves selection, variables,
// inheritance and overrides using Buildx. Empty names use Buildx's file discovery;
// empty targets select the default group. No image storage or daemon is needed.
func Resolve(ctx context.Context, names, targets, overrides []string, stdin io.Reader) (*Definition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if stdin == nil && slices.Contains(names, "-") {
		return nil, fmt.Errorf("stdin is required when reading a bake definition from -")
	}
	files, err := buildxbake.ReadLocalFiles(names, stdin, nil)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("couldn't find a bake definition")
	}
	if len(targets) == 0 {
		targets = []string{"default"}
	}
	defaults := map[string]string{
		"BAKE_CMD_CONTEXT":    "cwd://",
		"BAKE_LOCAL_PLATFORM": platforms.Format(platforms.DefaultSpec()),
	}
	ts, gs, err := buildxbake.ReadTargets(ctx, files, slices.Clone(targets), overrides, defaults, nil, &buildxbake.EntitlementConf{})
	if err != nil {
		return nil, err
	}
	return &Definition{Groups: gs, Targets: ts}, nil
}

// Build is one validated target. Arguments can be passed to the build command's
// option parser without shell interpretation. Targets execute in Name order.
type Build struct {
	Name      string
	Arguments []string
}

// Builds validates every selected target before returning a deterministic build
// plan. Unsupported settings are errors, never silently ignored. It does not
// access the contexts; --print therefore works before contexts exist locally.
func (d *Definition) Builds() ([]Build, error) {
	builds := make([]Build, 0, len(d.Targets))
	for _, name := range slices.Sorted(maps.Keys(d.Targets)) {
		args, err := arguments(d.Targets[name])
		if err != nil {
			return nil, fmt.Errorf("bake target %q: %w", name, err)
		}
		builds = append(builds, Build{Name: name, Arguments: args})
	}
	return builds, nil
}

func arguments(t *buildxbake.Target) ([]string, error) {
	if t == nil {
		return nil, fmt.Errorf("missing target definition")
	}
	// Validate the serialized fields so newly added upstream settings fail closed
	// until their execution semantics have an explicit Buildah mapping.
	data, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for _, field := range slices.Sorted(maps.Keys(fields)) {
		switch field {
		case "description", "context", "dockerfile", "args", "labels", "tags", "target", "pull", "no-cache":
		default:
			return nil, fmt.Errorf("unsupported setting %q", field)
		}
	}
	contextDir := "."
	if t.Context != nil {
		contextDir = *t.Context
	}
	if strings.HasPrefix(contextDir, "cwd://") {
		contextDir = strings.TrimPrefix(contextDir, "cwd://")
		if contextDir == "" {
			contextDir = "."
		}
	}
	if !localPath(contextDir) {
		return nil, fmt.Errorf("only local directory contexts are supported: %q", contextDir)
	}
	contextDir, err = filepath.Abs(contextDir)
	if err != nil {
		return nil, err
	}
	dockerfile := "Dockerfile"
	if t.Dockerfile != nil && *t.Dockerfile != "" {
		dockerfile = *t.Dockerfile
	}
	if !localPath(dockerfile) {
		return nil, fmt.Errorf("only local Dockerfiles are supported: %q", dockerfile)
	}
	// Bake resolves Dockerfiles relative to the context, while build also checks
	// the working directory. An absolute path prevents picking the wrong file.
	if !filepath.IsAbs(dockerfile) {
		dockerfile = filepath.Join(contextDir, dockerfile)
	}
	// The build command's --file flag is a CSV-backed StringSlice. Encode one
	// entry so commas or quotes in a pathname cannot become extra Dockerfiles.
	var fileFlag strings.Builder
	writer := csv.NewWriter(&fileFlag)
	if err := writer.Write([]string{dockerfile}); err != nil {
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	args := []string{"--file=" + strings.TrimSuffix(fileFlag.String(), "\n")}
	for _, key := range slices.Sorted(maps.Keys(t.Args)) {
		if value := t.Args[key]; value != nil {
			args = append(args, "--build-arg="+key+"="+*value)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(t.Labels)) {
		if value := t.Labels[key]; value != nil {
			args = append(args, "--label="+key+"="+*value)
		}
	}
	for _, tag := range t.Tags {
		args = append(args, "--tag="+tag)
	}
	if t.Target != nil {
		args = append(args, "--target="+*t.Target)
	}
	if t.NoCache != nil {
		args = append(args, "--no-cache="+strconv.FormatBool(*t.NoCache))
	}
	if t.Pull != nil {
		policy := "missing"
		if *t.Pull {
			policy = "always"
		}
		args = append(args, "--pull="+policy)
	}
	return append(args, contextDir), nil
}

func localPath(value string) bool {
	return value != "" && value != "-" && !strings.Contains(value, "://") &&
		!strings.HasPrefix(value, "git@") && !strings.HasPrefix(value, "target:") &&
		!strings.HasPrefix(value, "docker-image:")
}
