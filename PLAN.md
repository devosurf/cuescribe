# Cuescribe Implementation Plan

## Product

Cuescribe is an open-source MIT CLI for local Markdown and JSON transcripts from YouTube URLs and local media files, with an audio-only URL download command for listening.

Primary install:

```sh
curl -fsSL https://cuescribe.dev/install.sh | sh
```

Primary usage:

```sh
cuescribe "https://youtube.com/watch?v=..."
cuescribe ./lecture.mp4 -o lecture.md
cuescribe URL --source audio
cuescribe URL --translate
cuescribe --list-formats URL
cuescribe download URL --audio-format mp3
```

## V1 Scope

- macOS Apple Silicon only.
- One input per run: one YouTube URL, best-effort `yt-dlp` URL, or one local media file.
- YouTube support is official; other `yt-dlp` URLs are best-effort.
- Local files use audio transcription only.
- No playlists, directories, batching, summaries, active livestreams, VAD, word timestamps, output templates, or inferred diarization.

## Core Pipeline

- Implement in Go with Cobra.
- Shell out without a shell to external tools:
  - `yt-dlp`
  - `ffmpeg`
  - `ffprobe`
  - `whisper-cli`
- Default backend: `whisper.cpp` through any compatible `whisper-cli` on PATH.
- Normalize audio to 16 kHz mono WAV temp files.
- Use `whisper-cli --output-json-full` as the audio parsing source.
- Use YouTube subtitles before audio when available:
  - manual subtitles
  - auto subtitles
  - audio fallback
- Request and parse VTT for subtitles. Add an SRT parser in v1 if cheap, but VTT is the required YouTube path.

## Source Selection

```sh
--source auto|subs|audio
--subs any|manual|auto
--lang auto|sv|en|Swedish
--translate
```

Defaults:

- `--source auto`
- `--subs any`
- `--lang auto`

Translate behavior:

1. Prefer English manual subtitles.
2. Then English auto subtitles.
3. Else download audio and run Whisper translate mode.

Translation means English only in v1.

Troubleshooting:

```sh
--list-formats
```

`--list-formats URL` prints yt-dlp's available formats for the input and exits.

## Audio Downloads

```sh
cuescribe download URL
cuescribe download URL --audio-format best|mp3|m4a
cuescribe download URL --audio-format mp3 -o car/ --mkdir
cuescribe download URL --audio-format m4a --cover channel
cuescribe download URL --audio-format mp3 --cover none
```

- Download one remote input for listening without transcription or summarization.
- Select `bestaudio/best` and extract audio using yt-dlp and ffmpeg. For explicit
  M4A, prefer native AAC/M4A when available before falling back to best audio.
- Default `best` preserves the source codec where possible; no 16 kHz mono
  normalization. `mp3` and `m4a` convert when needed with audio quality `0`
  (yt-dlp's highest VBR quality).
  `best` can yield Opus and is not an Apple Music compatibility guarantee;
  recommend M4A/AAC or MP3 for Apple Music and Doppler.
- Automatically embed text metadata in the audio, not just filenames or sidecars.
  Title and album are the exact full video title, preserving Unicode and
  punctuation. Artist and album artist are the channel display name, falling
  back to uploader—not detected performers or handles when a display name exists.
  Retain the original video URL in comment/source tags.
- Treat each clip as a single-track album with its own artwork; do not group
  all clips into a channel album. No album override flag.
- Support `--cover thumbnail|channel|none`, defaulting to `thumbnail`. Channel
  artwork uses an additional bounded channel lookup for the avatar, never the
  banner. Convert artwork to PNG without cropping or changing aspect ratio.
  Embed covers in MP3/M4A and supported best formats (Opus/Ogg/FLAC).
  `none` omits artwork, including any inherited cover, while retaining text tags.
- Missing/unavailable/invalid or unsupported artwork warns and preserves tagged
  audio. A text-metadata-writing failure is fatal. Artwork attachment uses
  stream-copy and a separate staged output so a failure cannot damage tagged audio.
- Require only `yt-dlp`, `ffmpeg`, and `ffprobe`; no Whisper or summary models.
- Reuse configured browser/profile cookies on YouTube metadata, download,
  and channel lookup calls. Re-scope consent for the lookup host; never attach
  browser cookies to artwork image requests or other hosts by default.
- Reject local inputs, playlists, and active/upcoming livestreams.
- Use the final postprocessed audio extension and existing title sanitization.
  Default output is a title-based file in the current directory.
- Support `-o FILE_OR_DIR`, `--mkdir`, `--force`, `--verbose`, and `--debug`.
  Reject binary stdout (`-o -`). Append the actual extension to extensionless
  explicit filenames and reject mismatching extensions rather than mislabeling
  audio. Refuse existing audio files unless `--force` is passed.
- Stage downloads and artwork in cache, publish audio only after successful
  extraction and text tagging, and remove temporary files on success or failure.
- Keep the transcript command's `--source audio` behavior unchanged.

## Output

Default output is timestamped Markdown:

```md
# Video Title

Source: ...
Uploader: ...
Duration: ...
Language: auto
Detected language: sv
Mode: subtitles-auto
Translated: no
Generated by: Cuescribe 0.1.0

## Chapters

[00:00:00] Intro

## Transcript

[00:00:12] Text...
```

Supported output flags:

```sh
--format markdown|json
--no-timestamps
--timestamp-links
-o, --output FILE_OR_DIR
--mkdir
--force
```

When `-o` is omitted, write a title-based output file in the current directory, such as `Video Title.md` or `Video Title.json`. Use `-o -` to print to stdout.

Default timestamped Markdown is one segment per line. `--no-timestamps` affects Markdown only and should produce readable paragraphs. JSON always keeps segment timing.

JSON has an independent schema version:

```json
{
  "schema_version": 1,
  "title": "...",
  "source": "...",
  "language": "sv",
  "mode": "subtitles-auto",
  "translated": false,
  "chapters": [],
  "segments": [
    {
      "start_ms": 12000,
      "end_ms": 18400,
      "speaker": null,
      "text": "..."
    }
  ]
}
```

## Speaker Labels

V1 preserves speaker labels that already exist:

- VTT cue speakers such as `<v John>`.
- Conservative text labels such as `Speaker 1:`.

V1 does not infer speaker diarization from audio.

Keep transcript data structured from the start:

```go
type Segment struct {
    Start   time.Duration
    End     time.Duration
    Text    string
    Speaker string
}
```

Add a `Diarizer` interface internally so optional SpeechBrain diarization can be added later without rewriting the CLI.

## Setup And Config

Config:

```sh
~/.config/cuescribe/config.toml
```

Data and models:

```sh
~/.local/share/cuescribe/
```

Install state:

```sh
~/.local/state/cuescribe/install.toml
```

Cache and logs:

```sh
~/.cache/cuescribe/
```

Setup commands:

```sh
cuescribe setup
cuescribe setup deps
cuescribe setup model
cuescribe setup cookies
cuescribe config model
cuescribe config summary
cuescribe config cookies
```

Default model: multilingual `small`, downloaded during setup, checksum verified, resumable, and stored by Cuescribe.

Downloads and long-running operations should provide concise terminal feedback by default:

- Use human-readable progress bars for release binary, model, and upgrade downloads.
- Use cli-spinners-style Unicode status/spinner output for metadata lookup, media download, audio normalization, transcription, subtitle parsing, and output writing.
- Keep generated Markdown/JSON clean; progress and status messages go to stderr during transcript generation.
- Avoid raw byte counter lines such as `123/456 bytes`.

Model picker:

- `tiny`
- `base`
- `small`
- `medium`
- `large-v3-turbo`

Advanced model names, including quantized models, can be supported through flags/config. Custom model path is allowed.

Summary model picker:

- `qwen3-1.7b`
- `qwen3-4b`
- `qwen3-8b`

`cuescribe config summary --model NAME` changes the summary model after setup, uses Cuescribe's managed model path for known Qwen models, and downloads the managed model if missing. `--path /path/to/model.gguf` supports custom local GGUF models.

## Cookies

Interactive setup asks for consent before enabling YouTube browser cookies. Cookie setup validates YouTube cookie access before saving an enabled cookie config.

Behavior:

- Detect installed browsers.
- Detect the macOS default browser for HTTPS and prefer it when it is supported and installed.
- Select one browser/profile.
- If Chrome is selected and multiple Chrome profiles are detected, list the profiles and prompt for one.
- Store only browser/profile, not cookies.
- Automatically attach configured browser cookies to every YouTube call.
- Do not attach cookies to non-YouTube URLs by default.
- Non-interactive setup leaves cookies disabled unless a browser/profile is explicitly provided.
- `--require-cookies` makes setup and doctor strict: cookies must be enabled and validated.
- `doctor --fix` can repair cookie configuration interactively after consent.

`yt-dlp` cookie syntax supports:

```text
BROWSER[+KEYRING][:PROFILE][::CONTAINER]
```

V1 supports browser/profile. Firefox containers can be future work or raw advanced pass-through.

## Installer

The shell installer should only:

1. Detect OS and architecture.
2. Download the release manifest.
3. Download the matching binary.
4. Verify SHA-256.
5. Install the binary.
6. Run `cuescribe setup --yes` by default.

`cuescribe setup` owns dependency, model, and cookie setup. The website install
command should produce a working install on machines with Homebrew by installing
missing required formulas without an extra flag.
If required tools are missing and Homebrew is not installed, the installer
should stop before downloading the binary, print the Homebrew install command,
and ask the user to rerun the website install command after Homebrew is ready.

Homebrew dependencies:

- `yt-dlp`
- `ffmpeg`
- `whisper-cpp`

Do not install Homebrew itself. If Homebrew is missing, print instructions and exit setup.

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

## Admin Commands

```sh
cuescribe doctor
cuescribe doctor --strict
cuescribe doctor --fix
cuescribe version
cuescribe version --json
cuescribe version --check
cuescribe upgrade
cuescribe uninstall
cuescribe completion zsh|bash|fish
```

`doctor` checks:

- Cuescribe version and platform.
- Dependency presence and required features.
- `ffprobe` availability as a warning-level check.
- Model presence and checksum.
- Config and install state.
- YouTube cookie status, with access validation when cookies are enabled.

`doctor` does not check for newer Cuescribe releases.

`uninstall --yes` removes binary and cache. `--purge` also removes config and downloaded models. It does not uninstall Homebrew dependencies, but prints optional cleanup instructions.

## Release

- First version: `0.1.0`.
- License: MIT.
- Public repo: `github.com/devosurf/cuescribe`.
- Homepage target: `cuescribe.dev` (registered).
- Homepage hosting: Coolify Git App connected to the repository.
- GitHub Actions builds `darwin-arm64`.
- Release manifest includes binary and model URLs plus SHA-256 checksums.
- Model manifest is committed/reviewed and version-pinned to app releases.
- macOS binary is unsigned but checksummed in v1.
- No telemetry.
- No automatic update checks during normal use.

## Error And Logging

Failure messages should be short and actionable:

```text
Error: yt-dlp is missing.
Fix: brew install yt-dlp
```

Raw child output appears only with `--verbose`.

Debug logs:

```sh
~/.cache/cuescribe/logs/<timestamp>.log
```

Logs may include source URLs and browser/profile names, but must not include raw cookies, auth tokens, or exported cookie contents.

## Testing

V1 test scope:

- filename sanitization
- output collision behavior
- VTT/SRT parsing and cleanup
- subtitle/source selection decision tree
- Markdown writer
- JSON writer
- config path/load/save
- manifest checksum validation
- fake external command integration tests for `yt-dlp`, `ffmpeg`, and `whisper-cli`

CI should not hit real YouTube. Use fixtures and fake command wrappers.

## Future Roadmap

- Linux and Intel Mac builds.
- Optional SpeechBrain diarization backend.
- MLX transcription backend.
- Playlist and batch mode.
- Local subtitle import and embedded subtitle tracks.
- VAD.
- Output templates.
- Offline mode.
- Signed and notarized macOS binaries.
- Homebrew tap.
