# Repository Guidelines

## Project Structure & Module Organization

Cuescribe is currently in planning; `README.md` describes the product and `PLAN.md` is the source of truth for v1 scope and implementation details. As code is added, keep the Go CLI entrypoint under `cmd/cuescribe/` and implementation packages under `internal/` so they remain private to the application. Use focused packages such as `internal/subtitles`, `internal/transcript`, `internal/config`, `internal/output`, and `internal/runner`. Store fixtures under `testdata/` and keep release or installer automation in `scripts/` or `.github/workflows/`.

## Build, Test, and Development Commands

No build scripts exist yet. Once the Go module is created, use standard Go commands:

- `go run ./cmd/cuescribe --help` runs the CLI locally.
- `go test ./...` runs all unit and integration tests.
- `go test ./... -race` checks concurrent code paths when relevant.
- `go vet ./...` catches common Go mistakes.
- `gofmt -w .` formats Go source before committing.

Do not make tests depend on real YouTube, network access, or installed models. Use fixtures and fake command wrappers for `yt-dlp`, `ffmpeg`, `ffprobe`, and `whisper-cli`.

## Coding Style & Naming Conventions

Use idiomatic Go: tabs via `gofmt`, short package names, exported identifiers only when needed outside a package, and clear error returns. Prefer structured data over parsing rendered text; transcript segments should remain timestamped objects until the output writer stage. CLI flags should match `PLAN.md` exactly, for example `--source auto|subs|audio`, `--format markdown|json`, and `--timestamp-links`.

## Testing Guidelines

Prioritize tests for the v1 behaviors listed in `PLAN.md`: subtitle parsing, source selection, filename sanitization, output collision handling, Markdown/JSON writers, config load/save, checksum validation, and fake external command integration. Name tests after behavior, for example `TestSelectSourcePrefersManualSubtitles` or `TestMarkdownWriterOmitsTimestamps`.

## Commit & Pull Request Guidelines

This workspace has no local Git history, so no existing commit convention could be verified. Until the upstream repository establishes one, use concise imperative commits or Conventional Commit prefixes, for example `feat: add VTT parser` or `test: cover output collision handling`.

Pull requests should include a short description, the commands run, linked issues when available, and sample CLI output for user-visible behavior changes. Include screenshots only for homepage or documentation UI changes.

## Security & Configuration Tips

Never log raw cookies, auth tokens, model checksums from untrusted sources, or exported browser cookie contents. Keep config in `~/.config/cuescribe/`, cache and logs in `~/.cache/cuescribe/`, and downloaded models under `~/.local/share/cuescribe/` as planned.
