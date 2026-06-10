# Cuescribe

Local Markdown and JSON transcripts for YouTube videos and media files.

Homepage: https://cuescribe.dev
Repository: https://github.com/devosurf/cuescribe
Hosting: Coolify Git App connected to the repository

See [PLAN.md](PLAN.md) for the v1 product scope.

## Status

Cuescribe is a Go CLI for macOS Apple Silicon. It supports one input per run:

- YouTube URLs with subtitle-first transcription.
- Best-effort `yt-dlp` URLs.
- Local media files through audio transcription.

The CLI shells out without a shell to `yt-dlp`, `ffmpeg`, `ffprobe`, and `whisper-cli`.

## Install

```sh
curl -fsSL https://cuescribe.dev/install.sh | sh
```

Installer flags:

```sh
--no-setup
--yes
--require-cookies
--cookies-browser BROWSER
--cookies-profile PROFILE
--install-dir DIR
--version VERSION
```

## Build From Source

```sh
go test ./...
go build -o cuescribe ./cmd/cuescribe
```

## Usage

```sh
cuescribe "https://youtube.com/watch?v=..."
cuescribe ./lecture.mp4 -o lecture.md
cuescribe URL --source audio
cuescribe URL --translate
cuescribe URL --format json -o transcript.json
cuescribe URL -o -
```

Common flags:

```sh
--source auto|subs|audio
--subs any|manual|auto
--lang auto|sv|en|Swedish
--format markdown|json
--no-timestamps
--timestamp-links
-o, --output FILE_OR_DIR
--mkdir
--force
--list-formats
```

When `-o` is omitted, Cuescribe writes a title-based file in the current directory, for example `Video Title.md`. Use `-o -` to print to stdout.
Use `--list-formats URL` to print yt-dlp's available formats for troubleshooting download errors.

## Setup And Admin

```sh
cuescribe setup
cuescribe setup deps
cuescribe setup model
cuescribe setup cookies --browser safari
cuescribe setup cookies --browser chrome --profile "Profile 1"
cuescribe doctor
cuescribe doctor --strict
cuescribe doctor --fix
cuescribe version --json
cuescribe upgrade
cuescribe uninstall --yes
```

Config lives at `~/.config/cuescribe/config.toml`, install state at `~/.local/state/cuescribe/install.toml`, models under `~/.local/share/cuescribe/`, and logs/cache under `~/.cache/cuescribe/`.

Interactive setup asks for consent before enabling YouTube browser cookies, validates cookie access, prefers the macOS default browser when supported, and lists Chrome profiles when more than one is available. Non-interactive setup leaves cookies disabled unless `--cookies-browser` is passed; use `--require-cookies` to fail setup when cookie access cannot be validated.

Downloads show human-readable progress bars, and transcript runs print concise status for metadata, download, normalization, transcription, and output steps.
