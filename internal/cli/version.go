package cli

import (
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

var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Displays the version of arctl.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("arctl version %s\n", version.Version)
		fmt.Printf("Git commit: %s\n", version.GitCommit)
		fmt.Printf("Build date: %s\n", version.BuildDate)
		serverVersion, err := apiClient.GetVersion()
		if err != nil {
			fmt.Printf("Error getting server version: %v\n", err)
			return
		}
		fmt.Printf("Server version: %s\n", serverVersion.Version)
		fmt.Printf("Server git commit: %s\n", serverVersion.GitCommit)
		fmt.Printf("Server build date: %s\n", serverVersion.BuildTime)
		if !semver.IsValid(serverVersion.Version) || !semver.IsValid(version.Version) {
			fmt.Printf("Server or local version is not a valid semantic version, not sure if update require: %s or %s\n", serverVersion.Version, version.Version)
			return
		}

		compare := semver.Compare("v"+version.Version, "v"+serverVersion.Version)
		switch compare {
		case 1:
			fmt.Println("\n-------------------------------")
			fmt.Printf("CLI version is newer than server version: %s > %s\n", version.Version, serverVersion.Version)
			fmt.Println("We recommend updating your server version")
		case -1:
			fmt.Println("\n-------------------------------")
			fmt.Printf("Server version is newer than local version: %s > %s\n", serverVersion.Version, version.Version)
			fmt.Println("We recommend updating your CLI version")
		case 0:
		}

	},
}
