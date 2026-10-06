# Cuescribe

Local Markdown and JSON transcripts for YouTube videos and media files, plus audio-only downloads for listening.

Homepage: https://cuescribe.dev
Repository: https://github.com/devosurf/cuescribe
Hosting: Coolify Git App connected to the repository

See [PLAN.md](PLAN.md) for the v1 product scope.

## Status

Cuescribe is a Go CLI for macOS Apple Silicon. It supports one input per run:

- YouTube URLs with subtitle-first transcription.
- Best-effort `yt-dlp` URLs.
- Local media files through audio transcription.
- Audio-only URL downloads without transcription or a Whisper model.

The CLI shells out without a shell to `yt-dlp`, `ffmpeg`, `ffprobe`, and `whisper-cli`.

## Install

```sh
curl -fsSL https://cuescribe.dev/install.sh | sh
```

The installer downloads Cuescribe, installs required Homebrew dependencies when
Homebrew is available, and downloads the recommended Whisper model.
If Homebrew is missing, it stops before installing Cuescribe and prints the
Homebrew install command to run first.

After installation, the installer prints the exact command to run and any PATH
step needed for the selected install directory.

Installer flags:

```sh
--no-setup
--no-dependency-install
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
cuescribe URL --summarize
cuescribe URL --format json -o transcript.json
cuescribe URL -o -
```

Keep URLs in quotes. Shells such as zsh treat the `?` in YouTube watch URLs as
a wildcard when it is unquoted.

Common flags:

```sh
--source auto|subs|audio
--subs any|manual|auto
--lang auto|sv|en|Swedish
--summarize
--summary-lang sv|en|...
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

The default `--source auto` prefers manual subtitles, then automatic subtitles.
If no compatible subtitles exist or subtitle downloading/parsing fails (including
YouTube HTTP 429 rate limits), Cuescribe reports the failure and falls back to
audio transcription with Whisper. `--source subs` fails instead of using audio;
canceling a run never starts an audio fallback. Use `--source audio` to skip
subtitles entirely.

Subtitle downloads and normalized audio live in temporary
`~/.cache/cuescribe/run-*` directories, which are removed on both success and
failure. yt-dlp's “Writing video subtitles to…” message announces the intended
path, not a completed download. Finished transcripts are written to the output
location, not those temporary directories.

`--summarize` adds a fully local, multilingual summary to the output using a small LLM (Qwen3 via llama.cpp). The summary is written in the transcript's language unless `--summary-lang` says otherwise. Run `cuescribe setup summary` once to download a summary model — setup recommends one sized for the machine's RAM (8 GB: qwen3-1.7b, 16 GB: qwen3-4b, 32 GB+: qwen3-8b). After setup, change it with `cuescribe config summary --model qwen3-4b`; Cuescribe downloads managed Qwen models when missing. Point at a custom GGUF with `cuescribe config summary --model custom --path /path/to/model.gguf`.

## Download Audio For Listening

```sh
# Best available audio, preserving the source codec where possible
cuescribe download "https://youtube.com/watch?v=..."

# High-quality MP3 for broad car stereo / player compatibility
cuescribe download "https://youtube.com/watch?v=..." --audio-format mp3

# AAC in an M4A file, or save MP3 into a directory
cuescribe download "https://youtube.com/watch?v=..." --audio-format m4a -o lecture.m4a
cuescribe download "https://youtube.com/watch?v=..." --audio-format mp3 -o car/ --mkdir

# Use the channel avatar rather than the video thumbnail
cuescribe download "https://youtube.com/watch?v=..." --audio-format m4a --cover channel

# Text tags only, without cover artwork
cuescribe download "https://youtube.com/watch?v=..." --audio-format mp3 --cover none
```

`download` selects the best available audio and extracts it with `yt-dlp` and
`ffmpeg`. `--audio-format best` is the default: it avoids forced re-encoding and
uses the resulting audio extension, such as `.opus` or `.m4a`. **Best is not an
Apple Music compatibility guarantee**; choose `m4a` (AAC) or `mp3` for Apple Music
and Doppler. Explicit M4A prefers an available native AAC/M4A stream to avoid
lossy Opus-to-AAC conversion; otherwise MP3/M4A convert when needed using
yt-dlp's highest VBR quality setting. Downloading never normalizes audio to
speech-quality WAV, runs Whisper, or generates a transcript.
It requires `yt-dlp`, `ffmpeg`, and `ffprobe`, but no transcription tools or models.
`--source audio` on a normal transcript run still means audio **transcription**.

Text tags are automatic and embedded in the audio file:

- **Title:** the full video title, including Unicode and punctuation, independent
  of filename sanitization.
- **Artist and album artist:** the channel display name, falling back to uploader
  when channel metadata is absent—not a detected performer or channel handle.
- **Album:** the full video title. Each clip is treated as a single-track album
  with its own artwork, rather than grouping all clips into a channel album.
  There is no album override flag.
- **Comment/source:** the original video URL.

`--cover thumbnail|channel|none` defaults to the video thumbnail. `channel` makes
an additional channel lookup and uses the avatar, never the banner. Artwork is
converted to PNG without cropping or changing its aspect ratio and embedded
in the audio, so it survives temporary-file cleanup. `none` keeps text tags and
omits artwork, including artwork inherited from the source. MP3, M4A, Opus, Ogg,
and FLAC support embedded covers; other source formats retain audio with a warning.
Missing, unavailable, or invalid artwork also warns without discarding tagged
audio. Metadata-writing failures stop the download before publishing output.

Output defaults to a sanitized title-based filename in the current directory.
Use `-o FILE_OR_DIR`, `--mkdir`, and `--force` as for transcripts. Existing audio
files are not overwritten without `--force`. An explicit filename without an
extension gets the actual audio extension; a mismatching extension is rejected.
Use `--audio-format` to choose an encoding, not just a filename extension.
Binary stdout (`-o -`), local-file conversion, playlists, and active livestreams
are not supported by `download`.

Configured browser cookies are automatically reused for YouTube metadata,
downloads, and channel lookups, never attached to other hosts by default.
Artwork image requests do not receive browser cookies. To configure cookies:

```sh
cuescribe setup cookies --browser chrome --profile "Profile 1"
```

An HTTP 403 does not by itself establish that cookies are required. If a download
fails, use `cuescribe --list-formats "URL"` or add `--verbose` to the download
command. Keep `yt-dlp` current; use `cuescribe doctor` to check configured cookie
access.

## Setup And Admin

```sh
cuescribe setup
cuescribe setup deps
cuescribe setup model
cuescribe setup summary
cuescribe setup cookies --browser safari
cuescribe setup cookies --browser chrome --profile "Profile 1"
cuescribe config summary --model qwen3-4b
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
