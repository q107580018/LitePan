# Repository Guidelines

## Project Structure & Module Organization

LitePan is a Go service with a Vue/TypeScript web client.

- `cmd/litepan/` contains the application entry point.
- `internal/` holds application services, storage, HTTP/API wiring, caching, mounts, and domain logic; keep package boundaries consistent with `.golangci.yml`.
- `drivers/` contains integrations for supported cloud-storage providers. `drivers/template/` is the starting point for a new provider.
- `pkg/` contains reusable, business-independent utilities.
- `web/src/` contains Vue views, routing, assets, and client-side code; `web/scripts/` contains build helpers.
- `docs/pictures/` stores documentation images. `data/`, `strm/`, and `mounts/` are runtime/data directories and should not receive generated development artifacts.

## Build, Test, and Development Commands

Run commands from the repository root unless noted:

```bash
make test                 # Go tests with the race detector
make lint                 # golangci-lint using .golangci.yml
make build                # Go build with FUSE support
make build-nofuse        # Go build without FUSE support
cd web && npm ci          # Install the locked frontend dependencies
cd web && npm run dev     # Start the Vite development server
cd web && npm run build   # Type-check, bundle, and compress frontend assets
make docker-up            # Build and start the local Compose stack
make docker-down          # Stop the Compose stack
```

Use `make lint-install` when `golangci-lint` is not installed. The Docker build compiles the frontend first and embeds the generated web output into the Go image.

## Coding Style & Naming Conventions

Format Go changes with `gofmt` and organize imports with standard Go tooling. Use idiomatic Go names: `PascalCase` for exported identifiers, `camelCase` for local values, and clear package names. Prefer small services and interfaces at package boundaries; avoid importing concrete drivers into `internal` packages where the linter forbids it. Follow existing Vue conventions: `PascalCase.vue` components/views, `camelCase` variables and functions, and TypeScript types that describe API payloads. Keep formatting and type-checking clean before submitting.

## Testing Guidelines

Go tests use the standard `testing` package and live beside implementation files as `*_test.go`; name tests `Test<Behavior>`. Run `make test`, and use focused commands such as `go test ./internal/cache/...` during iteration. Add regression coverage for service, storage, driver, and API behavior that changes. Frontend validation currently uses `npm run type-check` and `npm run build`; no separate frontend test suite is configured.

## Commit & Pull Request Guidelines

Recent commits use short, imperative-style subjects, commonly prefixed with `feat:`, `fix:`, or `chore:` (Chinese descriptions are also established). Keep each commit focused. Pull requests should explain the behavior change, list verification commands, mention configuration or migration effects, link related issues, and include screenshots or recordings for UI changes. Do not commit secrets, local runtime data, build output, or dependency-lock changes unrelated to the change.
