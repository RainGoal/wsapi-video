package assets

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

func inspectBytes(t *testing.T, data []byte, probe string) (mediaMetadata, error) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "media-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		t.Fatal(err)
	}
	return inspectMedia(file, int64(len(data)), probe)
}

func TestImagesRequireDecodablePayload(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	img.Set(1, 1, color.White)
	for _, format := range []string{"png", "jpeg", "gif"} {
		t.Run(format, func(t *testing.T) {
			var data bytes.Buffer
			var err error
			switch format {
			case "png":
				err = png.Encode(&data, img)
			case "jpeg":
				err = jpeg.Encode(&data, img, nil)
			case "gif":
				err = gif.Encode(&data, img, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := inspectBytes(t, data.Bytes(), ""); err != nil || got.kind != "image" || got.width != 16 || got.height != 12 {
				t.Fatalf("valid %s rejected: %+v %v", format, got, err)
			}
			// Find a prefix that still exposes dimensions but has no full image.
			for length := 1; length < data.Len(); length++ {
				if _, _, err := image.DecodeConfig(bytes.NewReader(data.Bytes()[:length])); err == nil {
					if _, err := inspectBytes(t, data.Bytes()[:length], ""); !errors.Is(err, ErrInvalidMedia) {
						t.Fatalf("accepted %s header without payload at %d: %v", format, length, err)
					}
					return
				}
			}
			t.Fatal("test did not exercise a metadata-only prefix")
		})
	}
}
