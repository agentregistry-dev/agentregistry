# pkg/cli — `arctl` command surface

Root wiring lives in `root.go`. Downstream binaries import this
package, register their own additions, and call `cli.Configure` /
`cli.Root` to assemble their own CLI.

## Flag extensibility pattern

When a subcommand flag is only meaningful once a downstream binary
registers additional resources, don't wire it by default. Instead,
construct the subcommand with its baseline flags only and expose an
exported `Enable<Capability>()` function that downstream calls after
registration.

OSS-only binaries don't call the hook — operators don't see the flag
in `--help` or get accept-and-ignore behavior. Downstream binaries
call it after `Register`.

### Worked example: `migrate.EnableSourceSelection`

`pkg/cli/db/migrate` ships the `--source` flag for disambiguating
per-source ops in multi-source binaries. Single-source binaries
infer the source.

- `migrate.NewCommand()` wires `--db-url` and subcommands only.
- `migrate.EnableSourceSelection()` attaches the `--source` flag.
- OSS root.go doesn't call it.
- Downstream root.go calls it after `Register`-ing its source.

### When to apply

- The flag has no operator-visible behavior in OSS alone.
- The downstream registration is already a touch point in `root.go`.
- Suppressing the flag in `--help` / cleaning up error messages is a
  real UX improvement.

Skip the pattern if the flag is meaningful in OSS by itself, or if
downstream needs to mutate the flag's value (use an option struct on
`NewCommand` instead).

### Call order

Each `Enable<Capability>()` documents its own preconditions inline;
the general shape is `NewCommand` → `Register` → `Enable…`.
