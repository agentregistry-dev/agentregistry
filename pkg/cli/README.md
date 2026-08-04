# pkg/cli

`Root` builds an `arctl` command tree from `Config`.

Downstream CLIs should put customization in the config they pass to `Root`:

```go
root := cli.Root(cli.Config{
	Version: version.Version,
	Auth: enterpriseAuthProvider{},
	Disabled: map[string]bool{
		"db migrate goto": true,
	},
	ExtraCommands: func(deps cliruntime.Deps) []*cobra.Command {
		return []*cobra.Command{
			runtime.NewCommand(deps),
			user.UserCommand(ctx, deps),
		}
	},
	ExtraMigrationSources: []migrate.Source{
		entlegacymigrate.EnterpriseSource(),
	},
})
```

Extra commands receive the same runtime, authentication provider, and kind
registry as built-in commands. Commands using an extended downstream client
should call `deps.Runtime.ResolveRegistryTarget` and pass the returned base URL
and token to that client; commands using the OSS API client can call
`deps.Runtime.RegistryClient` directly.

The OSS migration source is always registered first by `Root`. Extra migration
sources are appended in config order. When more than one source is present,
`db migrate` exposes `--source`; single-source CLIs omit it.
