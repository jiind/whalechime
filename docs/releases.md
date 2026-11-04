# Release packages and publishing

The `release` workflow validates and packages the tray app. Regular CI continues to run Go and browser tests. A release is published only when a valid version tag is explicitly pushed.

## Package targets

| Platform | Runner | Download format | Entry point |
| --- | --- | --- | --- |
| Linux amd64 | Ubuntu | `.tar.gz` | `whalechime` |
| Windows amd64 | Windows | `.zip` | `whalechime.exe` |
| macOS Intel | macOS Intel | `.tar.gz` | `Whalechime.app` |
| macOS Apple Silicon | macOS arm64 | `.tar.gz` | `Whalechime.app` |

Archives are named `whalechime_<version>_<os>_<arch>`. macOS uses `darwin` in filenames. Each archive contains its executable or app bundle, README, license, security guide, documentation, and a `release.json` recording the version, target, and source revision.

Packages are not developer-signed or notarized. OS security prompts and policies may apply. Building and testing on a hosted runner does not verify tray interaction or notification display on a user's desktop. Linux needs a StatusNotifier/AppIndicator-compatible tray and notification service. SSH mode requires OpenSSH locally and Docker CLI access remotely.

## Validate without publishing

Pull requests that change release workflows, packaging, Go dependencies, or application code run native Go checks and build all four archives. The normal `go` and `browser` checks also run. Download validation archives from the workflow's artifacts; they use `v0.0.0-dev` and are retained for seven days.

A manual run of the release workflow validates and builds packages but never publishes a release. For local packaging, use Python 3.11+ and Go 1.25+:

```sh
python3 -m unittest discover -s tests/release -p 'test_*.py' -v
python3 scripts/package-release.py --version v0.1.0-rc.1 --os linux --arch amd64
```

The output directory defaults to `dist/`. Windows uses the same script with `--os windows --arch amd64`. macOS targets must be built on macOS with Xcode command line tools and CGO. The script does not overwrite an existing archive.

## Publish an explicitly selected version

1. Merge the intended changes into `main` and confirm the current CI results.
2. Choose a new SemVer tag such as `v0.1.0` or `v0.1.0-rc.1`.
3. From the intended `main` commit, create and push the tag:

```sh
git switch main
git pull --ff-only
git tag -a v0.1.0-rc.1 -m "Release v0.1.0-rc.1"
git push origin v0.1.0-rc.1
```

These commands publish a version; do not run them merely to test the workflow.

The workflow checks that the tag is valid SemVer and points to a commit contained in `main`. Existing Go/browser CI and all native package jobs must succeed before publication. Only the publish job receives `contents: write`; build and pull-request jobs keep read-only permissions.

GitHub CLI verifies that the tag already exists, stages a draft release, uploads all four archives plus `SHA256SUMS`, and then publishes it with generated notes. Tags with a prerelease identifier are marked as prereleases and are not marked latest. The workflow does not replace existing releases or assets. If publication fails, inspect any draft before retrying.

## Verify downloads

Download `SHA256SUMS` and the selected archive from the same trusted release. On Linux, after downloading all listed files:

```sh
sha256sum --check SHA256SUMS
```

For a single download on macOS, compare `shasum -a 256 <archive>` with its manifest entry. On Windows, use `Get-FileHash <archive> -Algorithm SHA256`. Checksums detect differences from the published manifest; they do not replace trust in the repository and release source.
