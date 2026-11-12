package files

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/gif"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

const (
	// MaxEmojiBytes is the size limit for a custom emoji upload.
	MaxEmojiBytes = 256 << 10
	emojiSide     = 128
	// Owner 2 is reserved for emoji files in the attachment storage and trash.
	emojiOwner snowflake.ID = 2
)

// EmojiReference identifies an emoji in the shared attachment storage.
func EmojiReference(id snowflake.ID) Reference {
	return AttachmentReference(emojiOwner, id)
}

// PrepareEmoji fits static images into 128x128 and validates small animated
// GIFs while keeping their original bytes and timing.
func (d *Disk) PrepareEmoji(ctx context.Context, raw []byte) ([]byte, bool, error) {
	if len(raw) > MaxEmojiBytes {
		return nil, false, ErrTooLarge
	}
	info, err := inspect(raw)
	if err != nil {
		return nil, false, fmt.Errorf("files: inspect emoji: %w", err)
	}
	if info.format == "image/gif" {
		count, countErr := gifFrames(raw)
		if countErr != nil {
			return nil, false, countErr
		}
		if count > 1 {
			return d.prepareAnimatedEmoji(ctx, raw, info, count)
		}
	}
	weight := info.weight()
	if weight > decodeBudget {
		return nil, false, unsupported("needs %d MiB to decode", weight>>20)
	}
	if acquireErr := d.acquire(ctx, weight); acquireErr != nil {
		return nil, false, fmt.Errorf("files: reserve emoji decode: %w", acquireErr)
	}
	defer d.release(weight)
	src, err := codecs[info.format].decode(bytes.NewReader(raw))
	if err != nil {
		return nil, false, unsupported("%v", err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if side := max(w, h); side > emojiSide {
		w, h = max(1, w*emojiSide/side), max(1, h*emojiSide/side)
	}
	data, err := scalePNG(ctx, src, b, image.Rect(0, 0, w, h), info.jpeg.orientation)
	if err != nil {
		return nil, false, fmt.Errorf("files: encode emoji: %w", err)
	}
	return data, false, nil
}

// prepareAnimatedEmoji validates the complete animation under the shared
// memory budget. A logical screen and every frame must fit the emoji box.
func (d *Disk) prepareAnimatedEmoji(ctx context.Context, raw []byte, info imageInfo, count int) ([]byte, bool, error) {
	if info.cfg.Width > emojiSide || info.cfg.Height > emojiSide {
		return nil, false, unsupported("animated GIF must fit %dx%d", emojiSide, emojiSide)
	}
	frameBytes := int64(info.cfg.Width)*int64(info.cfg.Height)*4 + 2048
	weight := weightOverhead + frameBytes*int64(count)
	if weight > decodeBudget {
		return nil, false, unsupported("animation needs %d MiB to decode", weight>>20)
	}
	if err := d.acquire(ctx, weight); err != nil {
		return nil, false, fmt.Errorf("files: reserve animated emoji decode: %w", err)
	}
	defer d.release(weight)
	animation, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		return nil, false, unsupported("%v", err)
	}
	if len(animation.Image) != count {
		return nil, false, unsupported("inconsistent GIF frame count")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, fmt.Errorf("files: decode animated emoji: %w", err)
	}
	return raw, true, nil
}

// gifFrames walks GIF blocks without decoding pixels. The frame count is
// needed before DecodeAll can reserve its memory.
func gifFrames(raw []byte) (int, error) {
	const (
		screenEnd = 13
		extension = 0x21
		frame     = 0x2c
		trailer   = 0x3b
	)
	if len(raw) < screenEnd {
		return 0, unsupported("truncated GIF screen descriptor")
	}
	pos := screenEnd + gifColorTable(raw[10])
	count := 0
	for pos < len(raw) {
		switch raw[pos] {
		case trailer:
			if count > 0 {
				return count, nil
			}
			return 0, unsupported("GIF has no frames")
		case extension:
			pos += 2 // marker and extension label
		case frame:
			if len(raw)-pos < 10 {
				return 0, unsupported("truncated GIF frame descriptor")
			}
			pos += 11 + gifColorTable(raw[pos+9]) // descriptor, palette and LZW code size
			count++
		default:
			return 0, unsupported("invalid GIF block")
		}
		var ok bool
		pos, ok = gifSubBlocks(raw, pos)
		if !ok {
			return 0, unsupported("truncated GIF data")
		}
	}
	return 0, unsupported("GIF has no trailer")
}

// gifColorTable gives the byte size of an optional packed RGB table.
func gifColorTable(packed byte) int {
	if packed&0x80 == 0 {
		return 0
	}
	return 3 << (1 + uint(packed&7))
}

// gifSubBlocks skips the length-prefixed data through its zero terminator.
func gifSubBlocks(raw []byte, pos int) (int, bool) {
	for pos < len(raw) {
		n := int(raw[pos])
		pos++
		if n == 0 {
			return pos, true
		}
		pos += n
	}
	return pos, false
}
