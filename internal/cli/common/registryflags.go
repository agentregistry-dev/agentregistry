package common

import (
	"github.com/spf13/cobra"
)

// registryFlagNames lists the root-level persistent flags that are irrelevant
// for commands that operate purely offline (e.g. init, build, add-tool).
var registryFlagNames = []string{"registry-url", "registry-token"}

// HideRegistryFlags marks the inherited registry-url and registry-token flags
// as hidden so they do not appear in the --help output of commands that do not
// interact with the registry. Multiple commands can be passed at once.
func HideRegistryFlags(cmds ...*cobra.Command) {
	for _, cmd := range cmds {
		original := cmd.HelpFunc()
		cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
			for _, name := range registryFlagNames {
				if f := c.InheritedFlags().Lookup(name); f != nil {
					f.Hidden = true
				}
			}
			original(c, args)
		})
	}
}
