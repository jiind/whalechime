# Contributing

Whalechime is an early-stage local tray app for monitoring multiple Docker servers. Read the [requirements](docs/requirements.md) and [contributor instructions](AGENTS.md) before changing its behavior.

## Issues and branches

For a bug report, include reproduction steps, expected and actual behavior, OS, and Docker version. For a feature request, describe the use case and desired behavior. Remove credentials and sensitive log content. Report vulnerabilities through the [private reporting process](SECURITY.md).

Use lowercase English issue titles such as `fix: ...`, `feat: ...`, or `docs: ...`, and reuse existing labels. A branch for an issue should include its number and type, for example `3-ci-release-and-english-docs`, and be associated with the issue's Development section. Without an issue, use a name such as `fix/connection-timeout`.

## Development and checks

```sh
go run ./desktop
make vet
make test
make build
npm ci
npx playwright install chromium
npm run test:ui
python3 -m unittest discover -s tests/release -p 'test_*.py' -v
```

Public documentation and templates use English. Explanatory source comments use Korean. Format Go code with `gofmt`. Add a role comment above explicit functions and methods, with numbered comments for multiple processing stages. Use temporary data and local receivers instead of personal settings or real notification accounts in tests.

For macOS builds, install Xcode command line tools and enable CGO. Linux needs a compatible tray and notification service. See [release documentation](docs/releases.md) for native build targets and archive generation.

## Pull requests

Describe the resulting user behavior, link the related issue, and list checks and untested environments. `main` requires a pull request and successful `go` and `browser` checks. Changes to release-related code also run native packaging checks. Pull requests are squash-merged and their remote branches are deleted after merging.

Do not publish a version tag merely to test the release workflow. Pull requests and manual workflow runs build validation artifacts without publishing a release. See [the release process](docs/releases.md).

Contributions are distributed under the repository's [Apache License 2.0](LICENSE).
