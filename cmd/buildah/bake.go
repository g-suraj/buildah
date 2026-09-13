package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.podman.io/buildah/define"
	"go.podman.io/buildah/imagebuildah"
	"go.podman.io/buildah/pkg/bake"
	buildahcli "go.podman.io/buildah/pkg/cli"
	"go.podman.io/buildah/util"
)

func bakeInit() { rootCmd.AddCommand(newBakeCommand()) }

func newBakeCommand() *cobra.Command {
	var files, overrides []string
	var printOnly bool
	command := &cobra.Command{
		Use:     "bake [TARGET...]",
		Short:   "Build images from a Bake definition",
		GroupID: groupImages,
		Long:    "Resolve a local Bake definition and build selected targets sequentially. Unsupported build settings are rejected before any target is built.",
		RunE: func(cmd *cobra.Command, targets []string) error {
			definition, err := bake.Resolve(getContext(), files, targets, overrides, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if printOnly {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(definition)
			}
			builds, err := definition.Builds()
			if err != nil {
				return err
			}
			prepared, cleanup, err := prepareBakeBuilds(builds)
			defer cleanup()
			if err != nil {
				return err
			}
			if len(prepared) == 0 {
				return nil
			}
			store, err := getStore(cmd)
			if err != nil {
				return err
			}
			for _, build := range prepared {
				if err := getContext().Err(); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Building bake target %q\n", build.name)
				if _, _, err := imagebuildah.BuildDockerfiles(getContext(), store, build.options, build.containerfiles...); err != nil {
					return fmt.Errorf("bake target %q: %w", build.name, err)
				}
			}
			return nil
		},
	}
	command.Flags().StringArrayVarP(&files, "file", "f", nil, "Bake definition `file` (repeatable; - reads stdin)")
	command.Flags().StringArrayVar(&overrides, "set", nil, "override a target setting (`target.key=value`)")
	command.Flags().BoolVar(&printOnly, "print", false, "print the resolved definition without building")
	command.SetUsageTemplate(UsageTemplate())
	return command
}

type preparedBakeBuild struct {
	name           string
	options        define.BuildOptions
	containerfiles []string
}

// Prepare every target before opening storage or starting a build. Each target
// receives a fresh build flag set, preventing values leaking between targets.
func prepareBakeBuilds(builds []bake.Build) ([]preparedBakeBuild, func(), error) {
	var removeAll []string
	cleanup := func() {
		for _, path := range removeAll {
			os.RemoveAll(path)
		}
	}
	var prepared []preparedBakeBuild
	for _, build := range builds {
		command, results, err := newBuildCommand()
		if err != nil {
			return nil, cleanup, err
		}
		if err := command.ParseFlags(build.Arguments); err != nil {
			return nil, cleanup, fmt.Errorf("bake target %q: %w", build.Name, err)
		}
		options, files, paths, err := buildahcli.GenBuildOptions(command, command.Flags().Args(), results)
		removeAll = append(removeAll, paths...)
		if err == nil {
			for _, tag := range append([]string{options.Output}, options.AdditionalTags...) {
				if tag != "" {
					if _, err = util.VerifyTagName(tag); err != nil {
						break
					}
				}
			}
		}
		if err == nil {
			for _, file := range files {
				var info os.FileInfo
				info, err = os.Stat(file)
				if err != nil {
					break
				}
				if !info.Mode().IsRegular() || info.Size() == 0 {
					err = fmt.Errorf("Dockerfile %q must be a nonempty regular file", file)
					break
				}
			}
		}
		if err != nil {
			return nil, cleanup, fmt.Errorf("bake target %q: %w", build.Name, err)
		}
		options.DefaultMountsFilePath = globalFlagResults.DefaultMountsFile
		prepared = append(prepared, preparedBakeBuild{name: build.Name, options: options, containerfiles: files})
	}
	return prepared, cleanup, nil
}
