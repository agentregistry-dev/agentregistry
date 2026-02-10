package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/version"
)

var apiClient *client.Client

func SetAPIClient(client *client.Client) {
	apiClient = client
}

type VersionOutput struct {
	ArctlVersion     string `json:"arctl_version"`
	GitCommit        string `json:"git_commit"`
	BuildDate        string `json:"build_date"`
	ServerVersion    string `json:"server_version,omitempty"`
	ServerGitCommit  string `json:"server_git_commit,omitempty"`
	ServerBuildDate  string `json:"server_build_date,omitempty"`
	UpdateRecommendation string `json:"update_recommendation,omitempty"`
}

var jsonOutput bool

var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Displays the version of arctl.`,
	Run: func(cmd *cobra.Command, args []string) {
		output := VersionOutput{
			ArctlVersion: version.Version,
			GitCommit:    version.GitCommit,
			BuildDate:    version.BuildDate,
		}

		serverVersion, err := apiClient.GetVersion()
		if err == nil {
			output.ServerVersion = serverVersion.Version
			output.ServerGitCommit = serverVersion.GitCommit
			output.ServerBuildDate = serverVersion.BuildTime

			if semver.IsValid(version.EnsureVPrefix(serverVersion.Version)) && semver.IsValid(version.EnsureVPrefix(version.Version)) {
				compare := semver.Compare(version.EnsureVPrefix(version.Version), version.EnsureVPrefix(serverVersion.Version))
				switch compare {
				case 1:
					output.UpdateRecommendation = "CLI version is newer than server version. Consider updating the server."
				case -1:
					output.UpdateRecommendation = "Server version is newer than CLI version. Consider updating the CLI."
				}
			}
		}

		if jsonOutput {
			jsonBytes, err := json.MarshalIndent(output, "", "  ")
			if err != nil {
				fmt.Printf("Error marshaling JSON: %v\n", err)
				return
			}
			fmt.Println(string(jsonBytes))
			return
		}

		// Human-readable output
		fmt.Printf("arctl version %s\n", output.ArctlVersion)
		fmt.Printf("Git commit: %s\n", output.GitCommit)
		fmt.Printf("Build date: %s\n", output.BuildDate)

		if serverVersion != nil {
			fmt.Printf("Server version: %s\n", output.ServerVersion)
			fmt.Printf("Server git commit: %s\n", output.ServerGitCommit)
			fmt.Printf("Server build date: %s\n", output.ServerBuildDate)

			if output.UpdateRecommendation != "" {
				fmt.Println("\n-------------------------------")
				fmt.Println(output.UpdateRecommendation)
			}
		} else if err != nil {
			fmt.Printf("Error getting server version: %v\n", err)
		}
	},
}

func init() {
	VersionCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output version information in JSON format")
}
