package core

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExtractVideoCover(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	// Default mov muxing puts the moov atom at the end, exercising the
	// seekable-input requirement that rules out piping the video in.
	videoPath := filepath.Join(t.TempDir(), "src.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error",
		"-f", "lavfi", "-i", "testsrc=duration=1:size=64x64:rate=5",
		"-pix_fmt", "yuv420p", "-y", videoPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg generate test video: %v (%s)", err, out)
	}
	video, err := os.ReadFile(videoPath)
	if err != nil {
		t.Fatalf("read test video: %v", err)
	}

	cover, err := ExtractVideoCover(context.Background(), video, "mp4")
	if err != nil {
		t.Fatalf("ExtractVideoCover() error = %v", err)
	}
	if len(cover) < 2 || !bytes.HasPrefix(cover, []byte{0xFF, 0xD8}) {
		t.Fatalf("cover is not JPEG (first bytes: % x)", cover)
	}

	t.Run("undecodable bytes return error", func(t *testing.T) {
		if _, err := ExtractVideoCover(context.Background(), []byte("not a video"), "mp4"); err == nil {
			t.Fatal("expected error for undecodable input")
		}
	})
}
