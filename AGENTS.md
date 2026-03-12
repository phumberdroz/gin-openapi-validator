# Repository Guidelines

## Project Structure & Module Organization
- `pkg/gin-openapi-validator/` contains the public library code and its tests.
  - Core middleware and error handling live in `validator.go` and `validationerror.go`.
  - Package docs are in `doc.go`.
  - Test fixtures like `petstore.yaml` are stored alongside tests and embedded via `go:embed`.
- There is no `cmd/` directory; this repo is a library module (`go.mod` defines the module path).

## Build, Test, and Development Commands
- `go test ./...` runs the full test suite across the module.
- `go test ./pkg/gin-openapi-validator -run TestBadRequests` runs a focused test by name.
- `go test -v ./pkg/gin-openapi-validator` runs verbose tests for the main package.
- `gofmt -w pkg/` formats all Go sources in the package.

## Coding Style & Naming Conventions
- Use Go standard formatting (tabs, gofmt). Run `gofmt` on changed files.
- Package name is `ginopenapivalidator` (no underscores); new files should use the same package.
- Exported identifiers should be PascalCase; unexported helpers should be lowerCamelCase.

## Testing Guidelines
- Tests use Go’s `testing` package with `testify` assertions (`assert`, `require`).
- Test files follow the `*_test.go` convention and live next to the code they test.
- Keep new tests table-driven where it improves coverage (see `TestBadRequests`).
- Run `go test ./...` before submitting changes.

## Commit & Pull Request Guidelines
- Git history is minimal and uses short, sentence-case messages (e.g., “Initial commit”).
  - Keep commit subjects short and descriptive; there is no strict convention enforced.
- PRs should include:
  - A brief description of the behavior change.
  - Any relevant test commands and results (e.g., `go test ./...`).
  - Sample requests/responses if the middleware behavior changes.

## Security & Configuration Tips
- The validator loads OpenAPI specs from bytes; avoid committing sensitive API specs.
- If you add new validation options, keep defaults safe and document any behavior changes.
