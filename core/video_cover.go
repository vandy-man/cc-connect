package core

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// ExtractVideoCover extracts the first frame of a video as a JPEG using
// ffmpeg, for use as a video message cover/thumbnail (e.g. Feishu media
// messages carry the cover in image_key).
//
// The video bytes are written to a temp file instead of being piped in:
// ffmpeg's mp4 demuxer needs to seek back to the moov atom, which sits at
// the end of most non-faststart files and is unreachable over a pipe.
func ExtractVideoCover(ctx context.Context, video []byte, srcExt string) ([]byte, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: install ffmpeg to enable video cover extraction")
	}

	ext := strings.ToLower(strings.TrimPrefix(srcExt, "."))
	if ext == "" {
		ext = "mp4"
	}

	inFile, err := os.CreateTemp("", "cc-connect-video-*."+ext)
	if err != nil {
		return nil, fmt.Errorf("create temp video file: %w", err)
	}
	inName := inFile.Name()
	defer os.Remove(inName)
	if _, err := inFile.Write(video); err != nil {
		inFile.Close()
		return nil, fmt.Errorf("write temp video file: %w", err)
	}
	inFile.Close()

	outFile, err := os.CreateTemp("", "cc-connect-cover-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("create temp cover file: %w", err)
	}
	outName := outFile.Name()
	outFile.Close()
	defer os.Remove(outName)

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-v", "error",
		"-i", inName,
		"-frames:v", "1",
		"-q:v", "3",
		"-f", "image2",
		"-y", outName,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg cover extraction failed: %w (stderr: %s)", err, stderr.String())
	}

	cover, err := os.ReadFile(outName)
	if err != nil {
		return nil, fmt.Errorf("read extracted cover: %w", err)
	}
	if len(cover) == 0 {
		return nil, fmt.Errorf("ffmpeg produced no cover frame")
	}

	slog.Debug("video cover extracted", "size", len(cover), "video_size", len(video), "ext", ext)
	return cover, nil
}
