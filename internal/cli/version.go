package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	"github.com/agentregistry-dev/agentregistry/internal/version"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
)

func NewVersionCommand(deps cliruntime.Deps) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   cliruntime.CommandVersion,
		Short: "Show version information",
		Long:  `Displays the version of arctl.`,
		Run: func(cmd *cobra.Command, args []string) {
			output := struct {
				CLI                  arv0.VersionBody  `json:"cli"`
				Server               *arv0.VersionBody `json:"server,omitempty"`
				UpdateRecommendation string            `json:"update_recommendation,omitempty"`
			}{
				CLI: arv0.VersionBody{
					Version:   version.Version,
					GitCommit: version.GitCommit,
					BuildTime: version.BuildDate,
				},
			}

			c, err := deps.Runtime.RegistryClient(cmd.Context())
			if err == nil {
				if serverVersion, serverErr := c.GetVersion(); serverErr == nil {
					output.Server = serverVersion
					if semver.IsValid(version.EnsureVPrefix(serverVersion.Version)) && semver.IsValid(version.EnsureVPrefix(output.CLI.Version)) {
						switch semver.Compare(version.EnsureVPrefix(output.CLI.Version), version.EnsureVPrefix(serverVersion.Version)) {
						case 1:
							output.UpdateRecommendation = "CLI version is newer than server version. Consider updating the server."
						case -1:
							output.UpdateRecommendation = "Server version is newer than CLI version. Consider updating the CLI."
						}
					}
				}
			}

			if jsonOutput {
				jsonBytes, jsonErr := json.MarshalIndent(output, "", "  ")
				if jsonErr != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "Error marshaling JSON: %v\n", jsonErr)
					return
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(jsonBytes))
				return
			}

			fmt.Fprintf(cmd.OutOrStdout(), "arctl version %s\n", output.CLI.Version)
			fmt.Fprintf(cmd.OutOrStdout(), "Git commit: %s\n", output.CLI.GitCommit)
			fmt.Fprintf(cmd.OutOrStdout(), "Build date: %s\n", output.CLI.BuildTime)

			if output.Server == nil {
				return
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Server version: %s\n", output.Server.Version)
			fmt.Fprintf(cmd.OutOrStdout(), "Server git commit: %s\n", output.Server.GitCommit)
			fmt.Fprintf(cmd.OutOrStdout(), "Server build date: %s\n", output.Server.BuildTime)

			if output.UpdateRecommendation != "" {
				fmt.Fprintln(cmd.OutOrStdout(), "\n-------------------------------")
				fmt.Fprintln(cmd.OutOrStdout(), output.UpdateRecommendation)
			}
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output version information in JSON format")

	return cmd
}
