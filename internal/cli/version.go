package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	"github.com/agentregistry-dev/agentregistry/internal/cli/utils"
	"github.com/agentregistry-dev/agentregistry/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Displays the version of arctl.`,
	Run: func(cmd *cobra.Command, args []string) {
		// fetch api client at start to avoid ux "freeze" while waiting for the registry to be ready
		apiClient, apiClientErr := utils.EnsureRegistryConnection()

		// local version info
		fmt.Println("Local version:")
		fmt.Printf("arctl version %s\n", version.Version)
		fmt.Printf("Git commit: %s\n", version.GitCommit)
		fmt.Printf("Build date: %s\n", version.BuildDate)
		fmt.Println()

		// remote version info
		// optional: check server version if the registry is running
		fmt.Println("Remote version:")
		if apiClientErr != nil {
			fmt.Printf("Error getting registry connection: %v\n", apiClientErr)
			return
		}

		serverVersion, err := apiClient.GetVersion()
		if err != nil {
			fmt.Printf("Error getting server version: %v\n", err)
			return
		}
		fmt.Printf("Server version: %s\n", serverVersion.Version)
		fmt.Printf("Server git commit: %s\n", serverVersion.GitCommit)
		fmt.Printf("Server build date: %s\n", serverVersion.BuildTime)
		fmt.Println()

		// additional information
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

func init() {
	rootCmd.AddCommand(versionCmd)
}
