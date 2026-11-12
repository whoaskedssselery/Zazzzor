package files_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/files"
)

func TestPrepareEmojiFitsStaticImages(t *testing.T) {
	disk, err := files.NewDisk(t.TempDir(), files.Options{})
	if err != nil {
		t.Fatalf("create disk: %v", err)
	}
	src := image.NewPaletted(image.Rect(0, 0, 256, 64), color.Palette{color.Black, color.White})
	for _, format := range []string{"png", "jpeg", "gif"} {
		t.Run(format, func(t *testing.T) {
			var raw bytes.Buffer
			var encodeErr error
			switch format {
			case "png":
				encodeErr = png.Encode(&raw, src)
			case "jpeg":
				encodeErr = jpeg.Encode(&raw, src, nil)
			case "gif":
				encodeErr = gif.Encode(&raw, src, nil)
			}
			if encodeErr != nil {
				t.Fatalf("encode image: %v", encodeErr)
			}
			data, animated, prepErr := disk.PrepareEmoji(t.Context(), raw.Bytes())
			if prepErr != nil {
				t.Fatalf("prepare emoji: %v", prepErr)
			}
			cfg, decodeErr := png.DecodeConfig(bytes.NewReader(data))
			if decodeErr != nil || animated || cfg.Width != 128 || cfg.Height != 32 {
				t.Errorf("dimensions, animated, error = %dx%d, %t, %v, want 128x32 false nil", cfg.Width, cfg.Height, animated, decodeErr)
			}
		})
	}
}

func TestPrepareEmojiPreservesAnimatedGIF(t *testing.T) {
	disk, err := files.NewDisk(t.TempDir(), files.Options{})
	if err != nil {
		t.Fatalf("create disk: %v", err)
	}
	for _, size := range []int{32, 129} {
		frame := image.NewPaletted(image.Rect(0, 0, size, size), color.Palette{color.Black, color.White})
		var raw bytes.Buffer
		if err := gif.EncodeAll(&raw, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{3, 4}}); err != nil {
			t.Fatalf("encode GIF: %v", err)
		}
		got, animated, err := disk.PrepareEmoji(t.Context(), raw.Bytes())
		if size == 32 {
			if err != nil || !animated || !bytes.Equal(got, raw.Bytes()) {
				t.Errorf("animated GIF changed or failed: animated = %t, error = %v", animated, err)
			}
		} else if err == nil {
			t.Error("oversized animated GIF accepted")
		}
	}
}

func TestPrepareEmojiAcceptsWebP(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(tinyWebP)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	disk, err := files.NewDisk(t.TempDir(), files.Options{})
	if err != nil {
		t.Fatalf("create disk: %v", err)
	}
	data, animated, err := disk.PrepareEmoji(t.Context(), raw)
	if err != nil || animated {
		t.Fatalf("prepare WebP = animated %t, error %v, want static image", animated, err)
	}
	if _, err = png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("decode stored PNG: %v", err)
	}
}
