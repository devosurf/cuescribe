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
```

## Setup And Admin

```sh
cuescribe setup
cuescribe setup deps
cuescribe setup model
cuescribe setup cookies --browser safari
cuescribe doctor
cuescribe version --json
```

Config lives at `~/.config/cuescribe/config.toml`, models under `~/.local/share/cuescribe/`, and logs/cache under `~/.cache/cuescribe/`.
