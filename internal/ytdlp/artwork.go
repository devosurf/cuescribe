package ytdlp

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devosurf/cuescribe/internal/config"
	"github.com/devosurf/cuescribe/internal/runner"
)

type Thumbnail struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// EmbedCover attaches artwork without re-encoding audio. Any failure leaves the
// tagged source intact, allowing the caller to warn and still publish the audio.
func EmbedCover(ctx context.Context, r runner.CommandRunner, source string, md Metadata, cover string, cookies config.CookieConfig) error {
	if cover == "none" {
		return nil
	}
	imageURL, err := coverURL(ctx, r, md, cover, cookies)
	if err != nil {
		return err
	}

	ext := strings.ToLower(filepath.Ext(source))
	switch ext {
	case ".mp3", ".m4a", ".mp4", ".flac", ".opus", ".ogg":
	default:
		return fmt.Errorf("cover embedding is unsupported for %s; use --audio-format m4a or mp3", ext)
	}
	stage, err := os.MkdirTemp(filepath.Dir(source), "cover-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	imagePath := filepath.Join(stage, "image")
	if err := fetchCover(ctx, imageURL, imagePath); err != nil {
		return fmt.Errorf("%s artwork unavailable: %w", cover, err)
	}
	pngPath := filepath.Join(stage, "cover.png")
	// Decode locally with no network protocols; conversion preserves dimensions.
	_, err = r.Run(ctx, "ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "file,pipe", "-f", "image2", "-pattern_type", "none", "-i", imagePath,
		"-frames:v", "1", "-c:v", "png", "-update", "1", pngPath)
	if err != nil {
		return fmt.Errorf("cannot convert cover to PNG: %w", err)
	}
	output := filepath.Join(stage, "audio"+ext)
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-i", source}
	if ext == ".opus" || ext == ".ogg" {
		metadataPath := filepath.Join(stage, "picture.ffmeta")
		if err := writePictureMetadata(metadataPath, pngPath); err != nil {
			return err
		}
		args = append(args, "-f", "ffmetadata", "-i", metadataPath, "-map", "0:a", "-c:a", "copy",
			"-map_metadata", "0", "-map_metadata:s:a", "0:s:a", "-map_metadata", "1")
	} else {
		args = append(args, "-i", pngPath, "-map", "0:a", "-map", "1:v", "-c", "copy",
			"-disposition:v:0", "attached_pic", "-metadata:s:v:0", "title=Album cover", "-metadata:s:v:0", "comment=Cover (front)")
		if ext == ".mp3" {
			args = append(args, "-id3v2_version", "3")
		}
	}
	args = append(args, output)
	if _, err := r.Run(ctx, "ffmpeg", args...); err != nil {
		return fmt.Errorf("cannot embed cover: %w", err)
	}
	if err := os.Rename(output, source); err != nil {
		return fmt.Errorf("cannot publish covered audio: %w", err)
	}
	return nil
}

func coverURL(ctx context.Context, r runner.CommandRunner, md Metadata, cover string, cookies config.CookieConfig) (string, error) {
	var imageURL string
	switch cover {
	case "thumbnail":
		imageURL = md.Thumbnail
		// yt-dlp sorts thumbnail alternatives from least to most preferred.
		if imageURL == "" && len(md.Thumbnails) > 0 {
			imageURL = md.Thumbnails[len(md.Thumbnails)-1].URL
		}
	case "channel":
		if md.ChannelURL == "" {
			return "", errors.New("channel URL is unavailable; no avatar embedded")
		}
		args := []string{"--ignore-config", "--dump-single-json", "--skip-download", "--flat-playlist", "--playlist-items", "0"}
		args = append(args, CookiesForInput(md.ChannelURL, cookies).YTDLPCookieArgs()...)
		args = append(args, md.ChannelURL)
		result, err := r.Run(ctx, "yt-dlp", args...)
		if err != nil {
			return "", fmt.Errorf("channel avatar lookup failed: %w", err)
		}
		var channel Metadata
		if err := json.Unmarshal(result.Stdout, &channel); err != nil {
			return "", fmt.Errorf("invalid channel artwork metadata: %w", err)
		}
		// YouTube's channel extractor labels the uncropped avatar explicitly.
		// Its thumbnail list also contains banners, which must never be used.
		for _, thumbnail := range channel.Thumbnails {
			if thumbnail.ID == "avatar_uncropped" {
				imageURL = thumbnail.URL
				break
			}
		}
	default:
		return "", fmt.Errorf("unsupported cover %q: use thumbnail, channel, or none", cover)
	}
	if imageURL == "" {
		return "", fmt.Errorf("%s artwork is unavailable; no cover embedded", cover)
	}
	return imageURL, nil
}

const maxCoverBytes = 16 << 20

func fetchCover(ctx context.Context, rawURL, dest string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errors.New("cover URL must use HTTP or HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	// Deliberately no cookie jar or browser credentials, including on redirects.
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("image server returned %s", resp.Status)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxCoverBytes+1))
	if err := errors.Join(copyErr, f.Close()); err != nil {
		return err
	}
	if n > maxCoverBytes {
		return fmt.Errorf("cover image exceeds %d MiB", maxCoverBytes>>20)
	}
	return nil
}

// Ogg stores artwork as a base64 FLAC picture block in a Vorbis comment, not a
// video stream. A metadata file avoids OS argument-size limits for large covers.
func writePictureMetadata(path, imagePath string) (err error) {
	image, err := os.Open(imagePath)
	if err != nil {
		return err
	}
	defer image.Close()
	info, err := image.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxCoverBytes {
		return fmt.Errorf("converted cover image exceeds %d MiB", maxCoverBytes>>20)
	}
	dimensions, err := png.DecodeConfig(image)
	if err != nil {
		return fmt.Errorf("invalid converted PNG cover: %w", err)
	}
	if _, err := image.Seek(0, io.SeekStart); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if _, err := io.WriteString(f, ";FFMETADATA1\nMETADATA_BLOCK_PICTURE="); err != nil {
		return err
	}
	encoded := base64.NewEncoder(base64.StdEncoding, f)
	if err := binary.Write(encoded, binary.BigEndian, []uint32{3, 9}); err != nil {
		return err
	}
	if _, err := io.WriteString(encoded, "image/png"); err != nil {
		return err
	}
	if err := binary.Write(encoded, binary.BigEndian, []uint32{0, uint32(dimensions.Width), uint32(dimensions.Height), 0, 0, uint32(info.Size())}); err != nil {
		return err
	}
	_, err = io.Copy(encoded, image)
	if err := errors.Join(err, encoded.Close()); err != nil {
		return err
	}
	_, err = io.WriteString(f, "\n")
	return err
}
