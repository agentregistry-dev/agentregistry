# pkg/cli

`Root` builds an `arctl` command tree from `Config`.

Downstream CLIs should put customization in the config they pass to `Root`:

```go
root := cli.Root(cli.Config{
	Version: version.Version,
	Auth: enterpriseAuthProvider{},
	Disabled: map[string]bool{
		cliruntime.CommandDaemon: true,
		"db migrate goto":        true,
	},
	ExtraCommands: []*cobra.Command{
		runtime.NewCommand(),
		user.UserCommand(ctx),
	},
	ExtraMigrationSources: []migrate.Source{
		entlegacymigrate.EnterpriseSource(),
	},
})
```

The OSS migration source is always registered first by `Root`. Extra migration
sources are appended in config order. When more than one source is present,
`db migrate` exposes `--source`; single-source CLIs omit it.
