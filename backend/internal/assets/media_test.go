package assets

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func atom(kind string, payload []byte) []byte {
	value := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(value[:4], uint32(len(value)))
	copy(value[4:8], kind)
	copy(value[8:], payload)
	return value
}
func syntheticMP4() []byte {
	ftyp := atom("ftyp", []byte{'m', 'p', '4', '2', 0, 0, 0, 0, 'i', 's', 'o', 'm', 'm', 'p', '4', '2'})
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:16], 1000)
	binary.BigEndian.PutUint32(mvhd[16:20], 5500)
	tkhd := make([]byte, 84)
	binary.BigEndian.PutUint32(tkhd[76:80], 1920<<16)
	binary.BigEndian.PutUint32(tkhd[80:84], 1080<<16)
	moov := atom("moov", append(atom("mvhd", mvhd), atom("trak", atom("tkhd", tkhd))...))
	return append(ftyp, moov...)
}

func TestMP4MetadataRejectsMalformedBounds(t *testing.T) {
	valid := syntheticMP4()
	width, height, duration, err := mp4Metadata(bytes.NewReader(valid), int64(len(valid)))
	if err != nil || width != 1920 || height != 1080 || duration != 5.5 {
		t.Fatalf("incorrect actual MP4 metadata: %d %d %f %v", width, height, duration, err)
	}
	for _, malformed := range [][]byte{valid[:len(valid)-1], {0, 0, 0, 1, 'm', 'o', 'o', 'v', 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, {0, 0, 0, 4, 'm', 'o', 'o', 'v'}} {
		if _, _, _, err = mp4Metadata(bytes.NewReader(malformed), int64(len(malformed))); !errors.Is(err, ErrInvalidMedia) {
			t.Fatalf("accepted malformed box: %x", malformed)
		}
	}
	name := filepath.Join(t.TempDir(), "video.bin")
	if err = os.WriteFile(name, valid, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	// Metadata alone is not a deliverable video: this has no media samples.
	if _, err := inspectMedia(file, int64(len(valid)), ""); !errors.Is(err, ErrInvalidMedia) {
		t.Fatalf("accepted metadata-only MP4: %v", err)
	}
}
func TestWebPActualDimensionsAndBounds(t *testing.T) {
	header := make([]byte, 30)
	copy(header, "RIFF")
	copy(header[8:], "WEBPVP8X")
	header[24] = 0xff
	header[25] = 3
	header[27] = 0xff
	header[28] = 1
	width, height, err := webpDimensions(header)
	if err != nil || width != 1024 || height != 512 {
		t.Fatalf("invalid WebP size %d %d %v", width, height, err)
	}
	if _, _, err = webpDimensions(header[:20]); !errors.Is(err, ErrInvalidMedia) {
		t.Fatal("accepted truncated WebP")
	}
}
