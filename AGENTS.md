# Repository Guidelines

## Project Structure

This is a Go and Vue 3 monorepo. `main.go` embeds `frontend/dist/` and starts the Gin server. Backend application code lives under `internal/`; reusable API, model, repository, service, transaction, and logging primitives live under `pkg/`. Wire dependency injection is defined in `cmd/wire.go`, with generated output in `cmd/wire_gen.go`. Vue source is under `frontend/src/`, public files are under `frontend/public/`, and generated frontend output is ignored at `frontend/dist/`.

Project identity is defined only in `project.json`. Generated metadata lives in `internal/projectmeta/` and `frontend/src/config/`. Do not edit generated files directly.

## Git Workflow

Before changing code, run `git status --short --branch` and preserve all existing user changes.

If Git is available but `.git/` does not exist, establish the initial code as the stable baseline:

```bash
git init -b main
git add .
git commit -m "chore: initialize project"
```

Never reinitialize an existing repository. Treat `main` as the stable baseline. Creating a separate branch is recommended for features, risky changes, and multi-file work, but it is not mandatory; small changes or work explicitly requested on `main` may be developed there directly. When using branches, create them from an up-to-date `main` with names such as `feature/<short-name>`, `fix/<short-name>`, or `chore/<short-name>`. Keep commits focused and use concise imperative subjects; `feat:`, `fix:`, `docs:`, and `chore:` prefixes are preferred.

## Quick Start

### 1. Detect the Environment

Run these checks before installing project dependencies:

```bash
git --version
go version
node --version
corepack --version
pnpm --version
```

Required versions are Go `1.25.4` from `go.mod`, Node `^20.19.0 || >=22.12.0` from `frontend/package.json`, and pnpm `10.23.0`. Node 22 LTS is the recommended default. Stop setup if a command is missing or its version is unsupported; do not hope that a later build will compensate.

Also identify the operating system and architecture before selecting installers:

```bash
uname -srm                 # macOS/Linux
cat /etc/os-release       # Linux distribution
```

On Windows, use `systeminfo` and `$env:PROCESSOR_ARCHITECTURE` in PowerShell.

### 2. Install Missing System Tools

System-wide installation, administrator access, global package changes, and proxy changes require explicit user approval. Explain the tool, version, command, and affected location before executing.

macOS with Homebrew:

```bash
brew install git go node
```

Debian or Ubuntu can install Git with:

```bash
sudo apt-get update
sudo apt-get install -y git
```

Fedora can install Git with:

```bash
sudo dnf install -y git
```

Distribution repositories may provide an outdated Go or Node release. Install Go `1.25.4` from [go.dev/dl](https://go.dev/dl/) and Node 22 LTS from [nodejs.org](https://nodejs.org/) or an already-installed nvm/Volta. With nvm:

```bash
nvm install 22
nvm use 22
```

Before installing a Go archive, match `uname -m` to the download architecture, verify the published SHA-256 checksum, and request approval before replacing `/usr/local/go` or changing a system PATH. Do not run unchecked `curl | sh` installers.

Windows with `winget`:

```powershell
winget install --id Git.Git -e
winget install --id GoLang.Go -e
winget install --id OpenJS.NodeJS.LTS -e
```

Open a new terminal after installation and rerun every version check. Never disable TLS verification or use an untrusted package mirror to bypass download errors.

### 3. Install pnpm

Use Corepack so pnpm stays pinned to the repository version:

```bash
corepack enable
corepack install --global pnpm@10.23.0
pnpm --version
```

If `corepack enable` fails because its shim directory is not writable, do not add `sudo`. Use `corepack pnpm <command>` temporarily or ask the user to repair the Node installation. If Corepack is absent, request approval before installing Corepack or a global pinned pnpm package.

### 4. Install Project Dependencies

From the repository root:

```bash
go mod download
cd frontend
pnpm install --frozen-lockfile
pnpm build
cd ..
```

The frozen lockfile is mandatory. Do not delete or regenerate the lockfile, disable integrity checks, or upgrade dependencies merely to make installation pass. The frontend must be built before Go because `main.go` embeds `frontend/dist/`.

### 5. Develop, Run, and Package

Run the complete local application:

```bash
pnpm --dir frontend build
go run .
```

Open `http://localhost:8888/static/`. For live frontend development, first ensure `frontend/dist/` exists, then use two terminals:

```bash
# Terminal 1
go run .

# Terminal 2
pnpm --dir frontend dev
```

Vite proxies `/api` to `http://localhost:8888`. Create release binaries with:

```bash
pnpm --dir frontend build
./build.sh
```

Regenerate Wire after editing providers:

```bash
go generate ./cmd
```

## Installation Failure Policy

Diagnose failures in this order:

1. Confirm the command exists and the terminal has refreshed its PATH.
2. Compare the actual version with repository requirements.
3. Check write permissions for the intended install/cache directory.
4. Check DNS, proxy, certificate, registry, and network errors.
5. Check operating system and CPU architecture compatibility.
6. Inspect `go env GOPATH GOMODCACHE GOPROXY` and `pnpm store path` for a usable offline cache.

After correcting one evidenced cause, retry once. Do not enter repeated install loops or silently switch registries. Corporate proxies and private mirrors must be supplied by the user; do not persist global proxy settings without approval.

If setup still cannot continue, stop dependent builds and report:

```text
Dependency setup is blocked: <dependency> <required-version>.
Attempted: <command>
Error: <key error and exit code>
Tried: <diagnostic or remediation>
Required action: <approval, proxy information, or manual installation>
Resume with: <verification command>
Unverified checks: <build/test list>
```

Never claim that builds or tests passed when dependency setup prevented them from running.

## Project Personalization

All project identity belongs in `project.json`: Go module, binary name, application name, display name, description, version, token prefix, and favicon path. Do not add branded literals to Go, Vue, HTML, shell scripts, or documentation.

After editing the manifest, regenerate and validate metadata:

```bash
go run ./tools/projectctl generate
go run ./tools/projectctl check
```

For a full project rename, use one command so Go imports and generated files remain consistent:

```bash
go run ./tools/projectctl rename \
  --module <module-path> \
  --binary <binary-name> \
  --app <application-name> \
  --display "<display name>" \
  --description "<application description>" \
  --token-prefix <token_prefix> \
  --favicon /favicon.svg
```

Replace the referenced favicon file if the brand asset itself changes. Existing runtime configuration and tokens are not migrated by project renaming.

## Coding and Testing Standards

Format Go with `gofmt`; use tabs, standard Go names, and `NewTypeName` constructors. Each API implements `pkg/api.IApi`. Keep business code in `internal/` and reusable primitives in `pkg/`. Vue component filenames use PascalCase; JavaScript identifiers use camelCase and two-space indentation. Do not hand-edit `frontend/dist/`, generated project metadata, or `cmd/wire_gen.go`.

Add Go tests beside production code as `*_test.go` with `TestXxx` names. There is no frontend unit-test runner yet, so frontend changes require a production build and manual route verification. Before handoff, run:

```bash
go run ./tools/projectctl check
go test ./...
go vet ./...
go build -buildvcs=false ./...
pnpm --dir frontend build
```

## UI Direction

The current pages demonstrate functionality only. Their layout, colors, spacing, and visual language are not a design requirement for future modules. Developers may replace the theme or page structure to suit the business domain. New UI must still be responsive, accessible, internally consistent, and complete across loading, empty, error, and success states.

## Pull Requests and Security

Pull requests should describe behavior changes, list verification commands, link relevant issues, and include screenshots for visible UI changes. Keep generated build output and unrelated refactors out of the diff.

Runtime configuration and generated tokens are stored in ignored `config/config.toml`. Never commit configuration files, access tokens, shared tokens, databases, or logs. Treat `/api/settings` changes as security-sensitive because that endpoint reads and writes token values.
