package assets

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestProbeRequiresDecodedMediaFrames(t *testing.T) {
	probe := os.Getenv("FFPROBE_PATH")
	if probe == "" {
		probe = "ffprobe"
	}
	if _, err := exec.LookPath(probe); err != nil {
		t.Skip("ffprobe unavailable; install it or set FFPROBE_PATH to run codec validation")
	}
	for _, sample := range []struct{ ext, kind string }{
		{"mp4", "video"}, {"webm", "video"}, {"webp", "image"},
		{"mp3", "audio"}, {"ogg", "audio"}, {"flac", "audio"},
	} {
		t.Run(sample.ext, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("testdata", "valid."+sample.ext))
			if err != nil {
				t.Fatal(err)
			}
			got, err := inspectBytes(t, content, probe)
			if err != nil || got.kind != sample.kind {
				t.Fatalf("valid decoded %s rejected: %+v %v", sample.ext, got, err)
			}
			if sample.kind != "audio" && (got.width != 64 || got.height != 36) {
				t.Fatalf("wrong decoded dimensions: %+v", got)
			}
		})
	}
	if _, err := inspectBytes(t, syntheticMP4(), probe); !errors.Is(err, ErrInvalidMedia) {
		t.Fatalf("metadata-only MP4 accepted: %v", err)
	}
	video, err := os.ReadFile(filepath.Join("testdata", "valid.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(video, []byte("mdat")) + 4
	if start < 4 || start >= len(video) {
		t.Fatal("fixture has no media payload")
	}
	clear(video[start:])
	if _, _, _, err := mp4Metadata(bytes.NewReader(video), int64(len(video))); err != nil {
		t.Fatalf("corruption fixture must retain valid container metadata: %v", err)
	}
	if _, err := inspectBytes(t, video, probe); !errors.Is(err, ErrInvalidMedia) {
		t.Fatalf("undecodable MP4 payload accepted: %v", err)
	}
	webp, err := os.ReadFile(filepath.Join("testdata", "valid.webp"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inspectBytes(t, webp[:30], probe); !errors.Is(err, ErrInvalidMedia) {
		t.Fatalf("WebP header without payload accepted: %v", err)
	}
}
