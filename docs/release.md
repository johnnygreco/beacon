# Installer and release process

Beacon releases are built by GoReleaser from `.goreleaser.yaml`. The expected
GitHub Release artifacts are:

- `beacon_darwin_amd64.tar.gz`
- `beacon_darwin_arm64.tar.gz`
- `beacon_linux_amd64.tar.gz`
- `beacon_linux_arm64.tar.gz`
- `checksums.txt`

Each archive contains the `beacon` binary plus `LICENSE` and
`THIRD_PARTY_NOTICES.md`.

Older releases may have fewer artifacts. `install.sh` requires the selected
release to include the current platform archive plus `checksums.txt`; otherwise
it exits before changing the installed binary.

## Installer verification

`install.sh` downloads the platform archive and `checksums.txt` from the same
GitHub Release. It verifies the archive's SHA-256 checksum before extracting or
writing files. Set `VERIFY_CHECKSUMS=0` only for local testing or emergency
debugging; doing so trusts the downloaded archive without verification.

The checksum file is not separately signed. The trust boundary is GitHub release
access plus HTTPS transport for both the artifact and checksum file.

Latest stable release discovery follows GitHub's `/releases/latest` redirect.
`INCLUDE_PRERELEASE=1` uses the GitHub Releases API because GitHub does not
provide an equivalent prerelease redirect; the selected release is still
validated by downloading and verifying the platform artifact before install.

When `INSTALL_CLICKHOUSE=1`, the installer also installs a pinned managed
ClickHouse binary when no `clickhouse` binary is already available. The default
pin is:

- `CLICKHOUSE_VERSION=24.12.6.70`
- `CLICKHOUSE_TAG=v24.12.6.70-stable`

Linux ClickHouse downloads use upstream `clickhouse-common-static` archives and
verify the matching `.sha512` sidecar from the ClickHouse GitHub Release before
extracting the binary. ClickHouse's macOS release assets for this pin do not
publish sidecar checksums, so macOS managed ClickHouse installs rely on the
pinned GitHub Release URL and HTTPS transport. Use `INSTALL_CLICKHOUSE=0` and
install ClickHouse through a package manager if you require a separately
verified or organization-managed ClickHouse binary on macOS.

The installer extracts into a temporary directory, verifies downloads before
installing, and only uses `sudo` when the target install directory is not
writable. `beacon` is installed after required downloads have verified.

## Publishing

`make publish VERSION=x.y.z` runs `scripts/publish.sh x.y.z`. The release host
must have:

- a clean, up-to-date `main` branch
- `GITHUB_TOKEN` or authenticated `gh`
- `goreleaser`
- `zig` for Linux CGO cross-builds

The script creates the local annotated tag, runs `goreleaser build --clean`, and
only pushes the tag after the local build succeeds. It refuses to continue if the
tag already exists locally or on `origin`.

If `goreleaser release --clean` fails after the tag was pushed, the script
attempts to roll back by deleting the GitHub Release and remote tag with:

```bash
gh release delete vX.Y.Z --cleanup-tag --yes
```

It also deletes the local tag. Set `PUBLISH_AUTO_ROLLBACK=0` to print the
rollback plan instead of attempting it. The rollback command path is covered by
`go test ./scripts`.

You can print the rollback plan without publishing:

```bash
scripts/publish.sh --rollback-plan x.y.z
```
