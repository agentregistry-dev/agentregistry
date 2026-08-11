# Releasing AgentRegistry

AgentRegistry releases are published by the GitHub Actions release workflow.
The supported release path is to tag a commit on `main`; release artifacts
should not be published manually from a developer workstation.

AgentRegistry follows Semantic Versioning:

- Major (`X.0.0`): breaking API or behavior changes.
- Minor (`X.Y.0`): backward-compatible features.
- Patch (`X.Y.Z`): backward-compatible fixes, including security fixes.

Release Git tags and container image tags include a leading `v`, such as
`v0.4.0`. Helm chart versions omit the prefix, such as `0.4.0`.

## Published artifacts

For a `vX.Y.Z` release, `.github/workflows/release.yaml` publishes:

- Multi-architecture Linux container images for `amd64` and `arm64`:
  - `ghcr.io/agentregistry-dev/agentregistry/server:vX.Y.Z`
- CLI binaries and individual SHA-256 files for:
  - Linux `amd64` and `arm64`
  - macOS `amd64` and `arm64`
  - Windows `amd64`
- The Helm chart at
  `oci://ghcr.io/agentregistry-dev/agentregistry/charts/agentregistry`
  with chart version `X.Y.Z` and application version `vX.Y.Z`.
- A GitHub Release with generated release notes, the CLI files, the packaged
  Helm chart, and the chart checksum file.

## Nightly images

The `Nightly Release` workflow checks `main` once per day at 02:00 UTC. When
`main` has changed since the previous successful nightly run, it publishes the
multi-architecture server image with two tags:

- `ghcr.io/agentregistry-dev/agentregistry/server:v0.0.0-alpha.<commit-sha>` is
  immutable and identifies the exact source commit.
- `ghcr.io/agentregistry-dev/agentregistry/server:latest-dev` moves to the most
  recent successful nightly image.

Nightly runs do not publish CLI binaries, Helm charts, Git tags, or GitHub
Releases. They can also be started manually from GitHub Actions, including when
`main` has not changed since the previous nightly. Manual runs must target the
`main` branch.

Prefer the immutable tag for repeatable deployments. To evaluate the moving
tag with the Helm chart, override the image tag and pull policy explicitly:

```bash
helm upgrade --install agentregistry \
  oci://ghcr.io/agentregistry-dev/agentregistry/charts/agentregistry \
  --set image.tag=latest-dev \
  --set image.pullPolicy=Always
```

## Cut a release

1. Choose the next version according to Semantic Versioning.
2. Confirm the release commit is on `main` and all required checks pass.
3. Review merged changes since the previous release for the generated release
   notes.
4. Create and push an annotated tag from the release commit:

   ```bash
   git fetch origin
   git tag -a vX.Y.Z origin/main -m "Release vX.Y.Z"
   git push origin vX.Y.Z
   ```

5. Monitor the `Release` workflow. The GitHub Release is created only after
   the server image and Helm chart have been published successfully.

Do not move or reuse a published release tag. If released content needs to
change, publish a new patch version.

## Verify a release

Confirm all jobs in the `Release` workflow succeeded, then verify each public
artifact:

```bash
gh release view vX.Y.Z
docker pull ghcr.io/agentregistry-dev/agentregistry/server:vX.Y.Z
helm show chart \
  oci://ghcr.io/agentregistry-dev/agentregistry/charts/agentregistry \
  --version X.Y.Z
```

The GitHub Release should contain each CLI binary next to its `.sha256` file,
plus `agentregistry-X.Y.Z.tgz` and `checksums.txt`. Compare downloaded files
against their published SHA-256 values before distributing them.

The Helm output should report `version: X.Y.Z` and `appVersion: vX.Y.Z`. The
default rendered server image must therefore use the same `vX.Y.Z` tag that
the container job published.

## Retry and recovery

First use GitHub Actions to rerun only failed jobs. Artifact publishing can be
partially complete, so inspect the GitHub Release, GHCR packages, and Helm OCI
chart before retrying.
