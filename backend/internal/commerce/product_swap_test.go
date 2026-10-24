package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wsapi-video/internal/config"
)

func swapProductFixture() Product {
	return Product{ID: "swap", Kind: "video", Model: "seedance-verified", Credits: 10, Capabilities: Capabilities{
		SupportsProductSwap: true, MaxImages: 3, MaxVideos: 1, Durations: []int{5, 10}, Resolutions: []string{"720p"}, AspectRatios: []string{"9:16"},
	}}
}

func TestProductSwapRequiresExplicitSeedanceCapability(t *testing.T) {
	p := swapProductFixture()
	if !validateProduct(Product{ID: p.ID, Name: "Swap", Kind: p.Kind, Model: p.Model, Credits: p.Credits, Capabilities: p.Capabilities}) {
		t.Fatal("valid Seedance capability rejected")
	}
	for _, mutate := range []func(*Product){func(p *Product) { p.Model = "another-video" }, func(p *Product) { p.Demo = true }, func(p *Product) { p.Capabilities.MaxVideos = 0 }, func(p *Product) { p.Capabilities.MaxImages = 0 }} {
		candidate := swapProductFixture()
		mutate(&candidate)
		if productSwapCapable(candidate) {
			t.Fatal("unsupported product accepted", candidate)
		}
	}
	changed := p.Capabilities
	changed.SupportsProductSwap = false
	if sameProductCapabilities(p.Capabilities, changed) {
		t.Fatal("tool capability must invalidate catalog verification")
	}
}

func TestProductSwapReferencesAndPrompt(t *testing.T) {
	swap := ProductSwapInput{SourceVideoID: "source", Images: []ProductSwapImage{{"front-image", "front"}, {"back-image", "back"}}, Target: "右手中的白瓶", Instructions: "保持瓶身比例", PreserveScene: true, AudioMode: "muted"}
	in := QuoteInput{ProductSwap: &swap, ReferenceAssetIDs: []string{"source", "front-image", "back-image"}}
	if !validSwapSnapshot(in, swapProductFixture()) {
		t.Fatal("valid snapshot rejected")
	}
	in.ReferenceAssetIDs[1], in.ReferenceAssetIDs[2] = in.ReferenceAssetIDs[2], in.ReferenceAssetIDs[1]
	if validSwapSnapshot(in, swapProductFixture()) {
		t.Fatal("reference ordering was lost")
	}
	text := swapPrompt(swap, nil)
	for _, expected := range []string{swap.Target, swap.Instructions, "正面", "背面", "原背景", "手指遮挡"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("prompt missing %q", expected)
		}
	}
	if err := validateSwapImages([]ProductSwapImage{{"image", "front"}, {"image", "back"}}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := validateSwapImages([]ProductSwapImage{{"image", "invented"}}); err == nil {
		t.Fatal("unknown angle accepted")
	}
}

func TestSwapVisionProtocolRejectsFailuresAndRedirects(t *testing.T) {
	observation := swapAnalysis{Target: "瓶子", Action: "拿起", Occlusion: "手指遮挡", ProductDescription: "蓝瓶", Risks: []string{"背面未知"}, MissingAngles: []string{"背面"}}
	for _, tc := range []struct {
		name    string
		status  int
		reason  string
		content any
		wantErr bool
	}{
		{"valid", 200, "stop", observation, false}, {"truncated", 200, "length", observation, true}, {"empty", 200, "stop", swapAnalysis{}, true}, {"failure", 503, "stop", observation, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Error("invalid protocol")
				}
				var input map[string]any
				if json.NewDecoder(r.Body).Decode(&input) != nil || input["model"] != "configured-vision" {
					t.Error("model not preserved")
				}
				w.WriteHeader(tc.status)
				raw, _ := json.Marshal(tc.content)
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(raw)}, "finish_reason": tc.reason}}})
			}))
			defer srv.Close()
			_, err := requestSwapAnalysis(context.Background(), srv.URL, "fixture-key", "configured-vision", []any{})
			if (err != nil) != tc.wantErr || calls.Load() != 1 {
				t.Fatal("unexpected result or retry", err, calls.Load())
			}
		})
	}
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer source.Close()
	if _, err := requestSwapAnalysis(context.Background(), source.URL, "fixture-key", "configured-vision", nil); err == nil || redirected.Load() != 0 {
		t.Fatal("redirect leaked request")
	}
}

type swapOwnedFixture struct {
	archiveFixture
	source []byte
}

func (f *swapOwnedFixture) OpenOwned(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.source)), nil
}

func TestSwapRealFrameExtractionAndAudio(t *testing.T) {
	ffmpeg := os.Getenv("FFMPEG_PATH")
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	if _, err := exec.LookPath(ffmpeg); err != nil {
		t.Skip("FFMPEG_PATH is required for media processing tests")
	}
	probe := os.Getenv("FFPROBE_PATH")
	if probe == "" {
		probe = "ffprobe"
	}
	if _, err := exec.LookPath(probe); err != nil {
		t.Skip("FFPROBE_PATH is required for media processing tests")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "source.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=160x240:r=10:d=1", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:v", "libx264", "-c:a", "aac", "-shortest", path)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{cfg: config.Config{FFmpegPath: ffmpeg, FFprobePath: probe}, assets: &swapOwnedFixture{source: raw}}
	frame, err := s.extractSwapFrame(ctx, path, .5)
	if err != nil {
		t.Fatal(err)
	}
	if image, err := jpeg.Decode(bytes.NewReader(frame)); err != nil || image.Bounds().Dx() > 768 || image.Bounds().Dy() > 768 {
		t.Fatal("invalid extracted frame", err)
	}
	for _, mode := range []string{"original", "muted"} {
		task := Task{Input: QuoteInput{ProductSwap: &ProductSwapInput{SourceVideoID: "source", AudioMode: mode}}}
		processed, err := s.processSwapAudio(ctx, task, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(mode, err)
		}
		output := processed.(*swapAudioFile)
		command := exec.CommandContext(ctx, probe, "-v", "error", "-select_streams", "a", "-show_entries", "stream=codec_type", "-of", "csv=p=0", output.Name())
		streams, err := command.Output()
		if err != nil || (strings.Contains(string(streams), "audio")) != (mode == "original") {
			t.Fatal(mode, "wrong audio streams", string(streams), err)
		}
		tmpDir := output.dir
		_ = processed.Close()
		if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
			t.Fatal("temporary media not removed")
		}
	}
}
