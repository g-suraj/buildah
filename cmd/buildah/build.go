package main

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.podman.io/buildah/imagebuildah"
	buildahcli "go.podman.io/buildah/pkg/cli"
	"go.podman.io/buildah/util"
)

func buildInit() {
	command, _, err := newBuildCommand()
	if err != nil {
		logrus.Error(err)
		os.Exit(1)
	}
	rootCmd.AddCommand(command)
}

// newBuildCommand creates independent flag state, also used when preparing Bake
// targets so defaults and validation stay identical to the build command.
func newBuildCommand() (*cobra.Command, buildahcli.BuildOptions, error) {
	buildDescription := `
  Builds an OCI image using instructions in one or more Containerfiles.

  If no arguments are specified, Buildah will use the current working directory
  as the build context and look for a Containerfile. The build fails if no
  Containerfile nor Dockerfile is present.`

	layerFlagsResults := buildahcli.LayerResults{}
	buildFlagResults := buildahcli.BudResults{}
	fromAndBudResults := buildahcli.FromAndBudResults{}
	userNSResults := buildahcli.UserNSResults{}
	namespaceResults := buildahcli.NameSpaceResults{}
	br := buildahcli.BuildOptions{
		LayerResults:      &layerFlagsResults,
		BudResults:        &buildFlagResults,
		UserNSResults:     &userNSResults,
		FromAndBudResults: &fromAndBudResults,
		NameSpaceResults:  &namespaceResults,
	}

	buildCommand := &cobra.Command{
		Use:     "build [CONTEXT]",
		Aliases: []string{"build-using-dockerfile", "bud"},
		Short:   "Build an image using instructions in a Containerfile",
		Long:    buildDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			return buildCmd(cmd, args, br)
		},
		Args: cobra.MaximumNArgs(1),
		Example: `buildah build
  buildah bud -f Containerfile.simple .
  buildah bud --volume /home/test:/myvol:ro,Z -t imageName .
  buildah bud -f Containerfile.simple -f Containerfile.notsosimple .`,
	}
	buildCommand.SetUsageTemplate(UsageTemplate())

	flags := buildCommand.Flags()
	flags.SetInterspersed(false)

	// build is a all common flags
	buildFlags := buildahcli.GetBudFlags(&buildFlagResults)
	buildFlags.StringVar(&buildFlagResults.Runtime, "runtime", util.Runtime(), "`path` to an alternate runtime. Use BUILDAH_RUNTIME environment variable to override.")

	layerFlags := buildahcli.GetLayerFlags(&layerFlagsResults)
	fromAndBudFlags, err := buildahcli.GetFromAndBudFlags(&fromAndBudResults, &userNSResults, &namespaceResults)
	if err != nil {
		return nil, br, fmt.Errorf("setting up build flags: %w", err)
	}

	flags.AddFlagSet(&buildFlags)
	flags.AddFlagSet(&layerFlags)
	flags.AddFlagSet(&fromAndBudFlags)
	flags.SetNormalizeFunc(buildahcli.AliasFlags)

	return buildCommand, br, nil
}

func buildCmd(c *cobra.Command, inputArgs []string, iopts buildahcli.BuildOptions) error {
	if c.Flag("logfile").Changed {
		logfile, err := os.OpenFile(iopts.Logfile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("opening log file: %w", err)
		}
		iopts.Logwriter = logfile
		defer iopts.Logwriter.Close()
	}

	options, containerfiles, removeAll, err := buildahcli.GenBuildOptions(c, inputArgs, iopts)
	if err != nil {
		return err
	}
	defer func() {
		for _, f := range removeAll {
			os.RemoveAll(f)
		}
	}()

	options.DefaultMountsFilePath = globalFlagResults.DefaultMountsFile

	store, err := getStore(c)
	if err != nil {
		return err
	}

	id, ref, err := imagebuildah.BuildDockerfiles(getContext(), store, options, containerfiles...)
	if err == nil && options.Manifest != "" {
		logrus.Debugf("manifest list id = %q, ref = %q", id, ref.String())
	}
	return err
}
