<!-- SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com> -->
<!-- SPDX-License-Identifier: MIT -->

# Contributing to TSDProxy

Thanks for your interest in contributing. TSDProxy is open source because people like you are willing to pitch in, and every contribution matters. Bug reports, documentation fixes, code, and ideas are all welcome.

## Getting Started

You will need these tools installed:

- **Go 1.26** or later
- **Bun** (JavaScript runtime for frontend builds)
- **Docker** (for running containers during development)
- **git**

### Using mise (optional)

[mise](https://mise.jdx.dev) can manage all required tools automatically. The project includes a `mise.toml` that pins Go, Bun, templ, golangci-lint, gotestsum, Hugo, Vite, and Air.

```bash
# Install mise (if not already installed)
curl https://mise.run | sh

# Activate mise in your shell
eval "$(mise activate bash)"  # or zsh/fish

# Install all project tools
mise install
```

The `mise.toml` also uses the **fnox** plugin (`jdx/mise-env-fnox`) for environment-specific tool configuration. mise handles this automatically on activation.

### Bootstrap

Clone the repository and run the bootstrap target to build frontend assets and generate templ templates:

```bash
git clone https://github.com/almeidapaulopt/tsdproxy.git
cd tsdproxy
make bootstrap
```

### Common Make Targets

| Target | What it does |
|--------|-------------|
| `make bootstrap` | First-time setup. Builds frontend and generates templ files. |
| `make dev` | Starts Docker containers, frontend dev server, and hot-reload backend. |
| `make build` | Compiles the Go binary to `./tmp/tsdproxy`. |
| `make test` | Runs the unit test suite. |
| `make test/e2e` | Runs end-to-end tests (requires Docker and a Tailscale auth key). |
| `make audit` | Full quality check: golangci-lint, staticcheck, go vet, deadcode, govulncheck, gosec, and tests. |
| `make docs` | Starts the Hugo docs site locally on port 1313. |

## Project Structure

```
cmd/server/main.go        Main server binary (also handles `healthcheck` subcommand)
internal/
  config/                 Configuration loading and file watching
  core/                   HTTP server, logging, health, sessions
  dashboard/              SSE dashboard routes and streaming
  model/                  Shared types (Config, PortConfig, ProxyStatus)
  proxymanager/           Central orchestrator wiring targets to proxies
  proxyproviders/         Proxy provider interface and Tailscale implementation
  targetproviders/        Target provider interface and Docker/List implementations
  ui/                     templ-generated UI components
web/                      Frontend: Vite + Bun, embedded in the binary via go:embed
docs/                     Hugo documentation site (separate go.mod)
e2e/                      End-to-end tests (build tag: e2e)
```

## Code Style

A few conventions to follow so your changes blend with the rest of the codebase:

- **SPDX headers are required** on every `.go` file:
  ```go
  // SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
  // SPDX-License-Identifier: MIT
  ```
  This is enforced by golangci-lint. Missing headers will fail CI.
- **Error wrapping**: use `fmt.Errorf("context: %w", err)` to add context to errors.
- **Logging**: use zerolog with structured fields: `log.With().Str("key", val).Logger()`.
- **Follow existing patterns**. When in doubt, look at how nearby code handles the same situation.
- **No type suppression hacks**: avoid `as any` casts or `@ts-ignore` comments to silence the type checker.

## Submitting Changes

1. Fork the repository.
2. Create a feature branch from `main`.
3. Make your changes with clear, descriptive commit messages.
4. Run `make audit` locally and fix any issues before pushing.
5. Open a pull request against `main` with a description of what changed and why.

CI runs on every PR. If checks fail, push a follow-up commit to the same branch to re-trigger them.

## Reporting Issues

Found a bug? Open a [bug report](https://github.com/almeidapaulopt/tsdproxy/issues/new?template=bug_report.md).

For troubleshooting help, check the [documentation](https://almeidapaulopt.github.io/tsdproxy/) first. Many common issues are covered there.

Have an idea? Open a [feature request](https://github.com/almeidapaulopt/tsdproxy/issues/new?template=feature_request.md).

## Ways to Contribute

Not sure where to start? Here are some ideas:

- **Bug fixes**: pick an open issue tagged `bug` and submit a PR.
- **Documentation**: typos, missing examples, unclear explanations. The docs live in `docs/`.
- **Examples**: share Docker Compose configs, label setups, or deployment patterns.
- **Tests**: more coverage is always welcome, especially for `internal/` packages.
- **Feature requests**: describe the problem you are trying to solve, not just the solution you want.
