package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

func generateTestMP4(t *testing.T) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	path := filepath.Join(t.TempDir(), "test.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error",
		"-f", "lavfi", "-i", "testsrc=duration=1:size=64x64:rate=5",
		"-pix_fmt", "yuv420p", "-y", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg generate test video: %v (%s)", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read test video: %v", err)
	}
	return data
}

// TestSendVideoAttachesCover verifies SendVideo uploads a first-frame cover
// and references it via image_key in the media message, and that a broken
// video body degrades gracefully to a bare file_key send.
func TestSendVideoAttachesCover(t *testing.T) {
	const appID = "cli_send_video"
	const appSecret = "secret-send-video"

	var mu sync.Mutex
	imageUploads := 0
	var lastMediaContent string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, map[string]any{
				"code":                0,
				"msg":                 "success",
				"expire":              7200,
				"tenant_access_token": "tenant-token",
			})
		case "/open-apis/im/v1/files":
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, map[string]any{
				"code": 0, "msg": "success",
				"data": map[string]any{"file_key": "file_v3_out"},
			})
		case "/open-apis/im/v1/images":
			mu.Lock()
			imageUploads++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, map[string]any{
				"code": 0, "msg": "success",
				"data": map[string]any{"image_key": "img_v3_cover_out"},
			})
		case "/open-apis/im/v1/messages":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			lastMediaContent, _ = body["content"].(string)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, map[string]any{
				"code": 0, "msg": "success",
				"data": map[string]any{"message_id": "om_out"},
			})
		default:
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, map[string]any{"code": 0, "msg": "success"})
		}
	}))
	defer srv.Close()

	newPlatform := func() *Platform {
		return &Platform{
			platformName: "feishu",
			domain:       srv.URL,
			appID:        appID,
			appSecret:    appSecret,
			client: lark.NewClient(appID, appSecret,
				lark.WithOpenBaseUrl(srv.URL),
				lark.WithHttpClient(srv.Client()),
			),
		}
	}

	t.Run("cover uploaded and referenced in media message", func(t *testing.T) {
		video := generateTestMP4(t)
		mu.Lock()
		imageUploads = 0
		mu.Unlock()

		p := newPlatform()
		err := p.SendVideo(context.Background(),
			replyContext{chatID: "oc_test"}, video, "mp4", "clip.mp4")
		if err != nil {
			t.Fatalf("SendVideo() error = %v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		if imageUploads != 1 {
			t.Errorf("image uploads = %d, want 1", imageUploads)
		}
		var media map[string]string
		if err := json.Unmarshal([]byte(lastMediaContent), &media); err != nil {
			t.Fatalf("media content is not JSON: %v (%q)", err, lastMediaContent)
		}
		if media["file_key"] != "file_v3_out" {
			t.Errorf("file_key = %q, want file_v3_out", media["file_key"])
		}
		if media["image_key"] != "img_v3_cover_out" {
			t.Errorf("image_key = %q, want img_v3_cover_out", media["image_key"])
		}
	})

	t.Run("broken video body still sends without cover", func(t *testing.T) {
		mu.Lock()
		imageUploads = 0
		mu.Unlock()

		p := newPlatform()
		fake := []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isomnot a real video")
		err := p.SendVideo(context.Background(),
			replyContext{chatID: "oc_test"}, fake, "mp4", "clip.mp4")
		if err != nil {
			t.Fatalf("SendVideo() error = %v, want graceful send without cover", err)
		}

		mu.Lock()
		defer mu.Unlock()
		if imageUploads != 0 {
			t.Errorf("image uploads = %d, want 0 for undecodable video", imageUploads)
		}
		var media map[string]string
		if err := json.Unmarshal([]byte(lastMediaContent), &media); err != nil {
			t.Fatalf("media content is not JSON: %v (%q)", err, lastMediaContent)
		}
		if media["file_key"] != "file_v3_out" {
			t.Errorf("file_key = %q, want file_v3_out", media["file_key"])
		}
		if media["image_key"] != "" {
			t.Errorf("image_key = %q, want empty for undecodable video", media["image_key"])
		}
	})
}
