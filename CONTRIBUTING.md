# Contributing to Docveta

Thanks for helping. Docveta aims to stay small and easy to self-host: one Go binary plus
PostgreSQL. Please keep changes in that spirit.

## Before you start

- Look at the [issues](https://github.com/anand34577/docveta/issues) for what's planned. The
  [worker protocol](docs/workers.md) and the [API spec](internal/api/openapi.yaml) show how the
  pieces fit together.
- For anything bigger than a bug fix, open an issue first so we can agree on the
  approach.
- Security problems: see [SECURITY.md](SECURITY.md), not the issue tracker.

## Development setup

See *Development* in the [README](README.md). Run everything before sending a PR:

```bash
make test                                    # Go, TypeScript and worker tests
DOCVETA_TEST_DATABASE_URL=postgres://… go test ./internal/app -run Integration -race
```

The integration test needs PostgreSQL 16+ and uses a throwaway schema.

## Rules of thumb

- **API changes** update [`internal/api/openapi.yaml`](internal/api/openapi.yaml) in the
  same PR. `go test ./internal/api` fails when routes and the spec disagree, and the
  integration test validates responses against it.
- **Authorization** lives in the services (`internal/spaces.Service.Require` and friends),
  not in HTTP handlers.
- **Schema changes** are new goose migrations in `internal/platform/db/migrations`;
  never edit a released migration.
- Format Go with `gofmt`; keep the TypeScript type-check clean (`npx tsc -b` in `web/`).
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/)
  (`fix: …`, `feat: …`), which the changelog is built from.

## Licensing

By contributing you agree that your contribution is licensed under the license of the
part you change: AGPL-3.0 for the server, web app and reference workers, Apache-2.0 for
`workers/sdk-python`.
