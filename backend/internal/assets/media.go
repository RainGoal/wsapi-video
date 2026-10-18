package assets

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

type mediaMetadata struct {
	kind, contentType, extension string
	width, height                int
	duration                     float64
}

func inspectMedia(file *os.File, size int64, probePath string) (mediaMetadata, error) {
	header := make([]byte, min(size, 512))
	if _, err := file.ReadAt(header, 0); err != nil {
		return mediaMetadata{}, ErrInvalidMedia
	}
	mime := http.DetectContentType(header)
	if mime == "application/ogg" {
		mime = "audio/ogg"
	} else if bytes.HasPrefix(header, []byte("fLaC")) {
		mime = "audio/flac"
	}
	metadata := mediaMetadata{contentType: mime}
	switch mime {
	case "image/png", "image/jpeg", "image/gif":
		if size > 64<<20 {
			return metadata, ErrTooLarge
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return metadata, err
		}
		configuration, format, err := image.DecodeConfig(file)
		if err != nil || !validDimensions(configuration.Width, configuration.Height) {
			return metadata, ErrInvalidMedia
		}
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			return metadata, err
		}
		decoded, decodedFormat, err := image.Decode(file)
		if err != nil || decodedFormat != format || decoded.Bounds().Dx() != configuration.Width || decoded.Bounds().Dy() != configuration.Height {
			return metadata, ErrInvalidMedia
		}
		metadata.kind = "image"
		metadata.width = configuration.Width
		metadata.height = configuration.Height
		metadata.extension = map[string]string{"png": ".png", "jpeg": ".jpg", "gif": ".gif"}[format]
	case "image/webp":
		width, height, err := webpDimensions(header)
		if err != nil {
			return metadata, err
		}
		metadata.kind = "image"
		metadata.width = width
		metadata.height = height
		metadata.extension = ".webp"
		if size > 64<<20 {
			return metadata, ErrTooLarge
		}
		if !validDimensions(width, height) {
			return metadata, ErrInvalidMedia
		}
		if err := probeMedia(file.Name(), probePath, &metadata); err != nil {
			return metadata, ErrInvalidMedia
		}
	case "video/mp4":
		metadata.kind = "video"
		metadata.extension = ".mp4"
		width, height, duration, err := mp4Metadata(file, size)
		if err != nil {
			return metadata, err
		}
		metadata.width = width
		metadata.height = height
		metadata.duration = duration
		if !validDimensions(width, height) {
			return metadata, ErrInvalidMedia
		}
		if err := probeMedia(file.Name(), probePath, &metadata); err != nil {
			return metadata, ErrInvalidMedia
		}
	case "audio/wave", "audio/x-wav":
		metadata.kind = "audio"
		metadata.contentType = "audio/wav"
		metadata.extension = ".wav"
		duration, err := wavDuration(file, size)
		if err != nil {
			return metadata, err
		}
		metadata.duration = duration
	case "video/webm", "audio/mpeg", "audio/ogg", "audio/flac":
		if mime == "video/webm" {
			metadata.kind = "video"
			metadata.extension = ".webm"
		} else {
			metadata.kind = "audio"
			metadata.extension = map[string]string{"audio/mpeg": ".mp3", "audio/ogg": ".ogg", "audio/flac": ".flac"}[mime]
		}
		if err := probeMedia(file.Name(), probePath, &metadata); err != nil {
			return metadata, ErrInvalidMedia
		}
	default:
		return metadata, ErrInvalidMedia
	}
	if metadata.kind == "image" || metadata.kind == "video" {
		if !validDimensions(metadata.width, metadata.height) {
			return metadata, ErrInvalidMedia
		}
	}
	if metadata.kind != "image" {
		if metadata.duration <= 0 || metadata.duration > 6*3600 || math.IsNaN(metadata.duration) || math.IsInf(metadata.duration, 0) {
			return metadata, ErrInvalidMedia
		}
	}
	if metadata.kind == "image" && size > 64<<20 {
		return metadata, ErrTooLarge
	}
	return metadata, nil
}

func validDimensions(width, height int) bool {
	return width > 0 && height > 0 && width <= 16384 && height <= 16384 && int64(width)*int64(height) <= 64_000_000
}

func webpDimensions(header []byte) (int, int, error) {
	if len(header) < 30 || string(header[:4]) != "RIFF" || string(header[8:12]) != "WEBP" {
		return 0, 0, ErrInvalidMedia
	}
	u24 := func(value []byte) int { return int(value[0]) | int(value[1])<<8 | int(value[2])<<16 }
	switch string(header[12:16]) {
	case "VP8X":
		return 1 + u24(header[24:27]), 1 + u24(header[27:30]), nil
	case "VP8 ":
		if len(header) < 30 || !bytes.Equal(header[23:26], []byte{0x9d, 0x01, 0x2a}) {
			return 0, 0, ErrInvalidMedia
		}
		return int(binary.LittleEndian.Uint16(header[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(header[28:30]) & 0x3fff), nil
	case "VP8L":
		if header[20] != 0x2f || len(header) < 25 {
			return 0, 0, ErrInvalidMedia
		}
		bits := binary.LittleEndian.Uint32(header[21:25])
		return int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1, nil
	}
	return 0, 0, ErrInvalidMedia
}

// ISO-BMFF metadata is read by seeking over bounded box headers; media payloads
// are never loaded into RAM. Duration comes from mvhd, dimensions from tkhd.
func mp4Metadata(reader io.ReaderAt, size int64) (int, int, float64, error) {
	var width, height int
	var duration float64
	boxes := 0
	var walk func(int64, int64, int) error
	walk = func(start, end int64, depth int) error {
		if depth > 8 {
			return ErrInvalidMedia
		}
		for offset := start; offset < end; {
			boxes++
			if boxes > 10000 || end-offset < 8 {
				return ErrInvalidMedia
			}
			var header [16]byte
			if _, err := reader.ReadAt(header[:8], offset); err != nil {
				return ErrInvalidMedia
			}
			boxSize := int64(binary.BigEndian.Uint32(header[:4]))
			headerSize := int64(8)
			kind := string(header[4:8])
			if boxSize == 1 {
				if end-offset < 16 {
					return ErrInvalidMedia
				}
				if _, err := reader.ReadAt(header[8:], offset+8); err != nil {
					return ErrInvalidMedia
				}
				large := binary.BigEndian.Uint64(header[8:])
				if large > uint64(end-offset) {
					return ErrInvalidMedia
				}
				boxSize = int64(large)
				headerSize = 16
			} else if boxSize == 0 {
				boxSize = end - offset
			}
			if boxSize < headerSize || boxSize > end-offset {
				return ErrInvalidMedia
			}
			bodyStart, bodySize := offset+headerSize, boxSize-headerSize
			switch kind {
			case "moov", "trak", "mdia":
				if err := walk(bodyStart, offset+boxSize, depth+1); err != nil {
					return err
				}
			case "mvhd":
				if bodySize < 20 {
					return ErrInvalidMedia
				}
				var body [32]byte
				readSize := min(int64(len(body)), bodySize)
				if _, err := reader.ReadAt(body[:readSize], bodyStart); err != nil {
					return ErrInvalidMedia
				}
				var scale uint32
				var ticks uint64
				if body[0] == 0 {
					scale = binary.BigEndian.Uint32(body[12:16])
					ticks = uint64(binary.BigEndian.Uint32(body[16:20]))
				} else if body[0] == 1 && bodySize >= 32 {
					scale = binary.BigEndian.Uint32(body[20:24])
					ticks = binary.BigEndian.Uint64(body[24:32])
				} else {
					return ErrInvalidMedia
				}
				if scale == 0 {
					return ErrInvalidMedia
				}
				duration = float64(ticks) / float64(scale)
			case "tkhd":
				if bodySize < 84 {
					return ErrInvalidMedia
				}
				var last [8]byte
				if _, err := reader.ReadAt(last[:], offset+boxSize-8); err != nil {
					return ErrInvalidMedia
				}
				w, h := int(binary.BigEndian.Uint32(last[:4])>>16), int(binary.BigEndian.Uint32(last[4:])>>16)
				if w > 0 && h > 0 {
					width = w
					height = h
				}
			}
			offset += boxSize
		}
		return nil
	}
	if err := walk(0, size, 0); err != nil {
		return 0, 0, 0, err
	}
	if width <= 0 || height <= 0 || duration <= 0 {
		return 0, 0, 0, ErrInvalidMedia
	}
	return width, height, duration, nil
}

func wavDuration(reader io.ReaderAt, size int64) (float64, error) {
	if size < 12 {
		return 0, ErrInvalidMedia
	}
	var rate, data uint32
	for offset := int64(12); offset+8 <= size; {
		var header [8]byte
		if _, err := reader.ReadAt(header[:], offset); err != nil {
			return 0, err
		}
		length := int64(binary.LittleEndian.Uint32(header[4:]))
		if length > size-offset-8 {
			return 0, ErrInvalidMedia
		}
		switch string(header[:4]) {
		case "fmt ":
			if length < 16 {
				return 0, ErrInvalidMedia
			}
			var format [16]byte
			if _, err := reader.ReadAt(format[:], offset+8); err != nil {
				return 0, err
			}
			rate = binary.LittleEndian.Uint32(format[8:12])
		case "data":
			data = uint32(length)
		}
		offset += 8 + length + (length % 2)
	}
	if rate == 0 || data == 0 {
		return 0, ErrInvalidMedia
	}
	return float64(data) / float64(rate), nil
}

func probeMedia(name, probePath string, metadata *mediaMetadata) error {
	if probePath == "" {
		probePath = "ffprobe"
	}
	if _, err := exec.LookPath(probePath); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Counting decoded frames rejects metadata-only files and corrupt payloads.
	// Restrict demuxer access to this local file; embedded remote URLs are not inputs.
	command := exec.CommandContext(ctx, probePath, "-v", "error", "-err_detect", "explode", "-max_pixels", "64000000", "-threads", "1", "-protocol_whitelist", "file", "-count_frames", "-show_entries", "stream=codec_type,width,height,duration,nb_read_frames:format=duration", "-of", "json", name)
	var output, diagnostics limitedBuffer
	command.Stdout = &output
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		return err
	}
	if diagnostics.Len() != 0 {
		return ErrInvalidMedia
	}
	var result struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Duration  string `json:"duration"`
			Frames    string `json:"nb_read_frames"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		return err
	}
	wantStream := metadata.kind
	if wantStream == "image" {
		wantStream = "video"
	}
	found := false
	for _, stream := range result.Streams {
		frames, _ := strconv.ParseInt(stream.Frames, 10, 64)
		if stream.CodecType == wantStream && frames > 0 {
			found = true
			metadata.width = stream.Width
			metadata.height = stream.Height
			metadata.duration, _ = strconv.ParseFloat(stream.Duration, 64)
			break
		}
	}
	if !found {
		return ErrInvalidMedia
	}
	if duration, _ := strconv.ParseFloat(result.Format.Duration, 64); duration > 0 {
		metadata.duration = duration
	}
	return nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(value []byte) (int, error) {
	if b.Len()+len(value) > 1<<20 {
		return 0, ErrInvalidMedia
	}
	return b.Buffer.Write(value)
}
