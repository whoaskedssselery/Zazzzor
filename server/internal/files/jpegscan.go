package files

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// maxJPEGScans bounds the SOS segments of a JPEG: each progressive scan
// walks the whole coefficient buffer, so thousands of tiny scans would keep
// a decode busy for minutes.
const maxJPEGScans = 100

var errBadJPEG = errors.New("files: malformed jpeg structure")

// jpegFacts is what the marker scan learns before any decoding.
type jpegFacts struct {
	progressive bool
	components  int
	scans       int
	orientation int // EXIF orientation 1..8 (1 when absent or invalid)
}

// scanJPEG walks the marker segments of a JPEG without decoding pixels. It
// stops counting at maxJPEGScans+1. The entropy-coded data after each SOS
// is skipped byte by byte (0xFF00 is a stuffed byte, 0xFFD0..D7 a restart
// marker), so the walk is linear in the file size.
func scanJPEG(b []byte) (jpegFacts, error) {
	f := jpegFacts{orientation: 1}
	if len(b) < 2 || b[0] != 0xFF || b[1] != 0xD8 {
		return f, errBadJPEG
	}
	exifSeen := false
	i := 2
	for {
		// Find the next marker, skipping fill bytes.
		if i >= len(b) || b[i] != 0xFF {
			return f, errBadJPEG
		}
		for i < len(b) && b[i] == 0xFF {
			i++
		}
		if i >= len(b) {
			return f, errBadJPEG
		}
		m := b[i]
		i++
		switch {
		case m == 0xD9: // EOI
			return f, nil
		case m == 0x01 || (m >= 0xD0 && m <= 0xD7): // no length
			continue
		}
		if i+2 > len(b) {
			return f, errBadJPEG
		}
		n := int(binary.BigEndian.Uint16(b[i:]))
		if n < 2 || i+n > len(b) {
			return f, errBadJPEG
		}
		payload := b[i+2 : i+n]
		i += n
		switch {
		case m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC: // SOFn
			f.progressive = m == 0xC2 || m == 0xC6 || m == 0xCA || m == 0xCE
			if len(payload) >= 6 {
				f.components = int(payload[5])
			}
		case m == 0xE1 && !exifSeen && bytes.HasPrefix(payload, []byte("Exif\x00\x00")):
			exifSeen = true
			f.orientation = exifOrientation(payload[6:])
		case m == 0xDA: // SOS: skip the entropy-coded data that follows
			f.scans++
			if f.scans > maxJPEGScans {
				return f, nil
			}
			for i < len(b) {
				if b[i] != 0xFF {
					i++
					continue
				}
				if i+1 >= len(b) {
					return f, errBadJPEG
				}
				if next := b[i+1]; next == 0x00 || (next >= 0xD0 && next <= 0xD7) {
					i += 2
					continue
				}
				break
			}
		}
	}
}

// exifOrientation reads tag 0x0112 from IFD0 of a TIFF block (the APP1
// payload after "Exif\0\0", at most 64 KiB by the JPEG segment length). Any
// malformed or out-of-range value means 1 (as stored).
func exifOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(t[2:]) != 42 {
		return 1
	}
	off := int(order.Uint32(t[4:]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	count := int(order.Uint16(t[off:]))
	entries := t[off+2:]
	for e := range count {
		if (e+1)*12 > len(entries) {
			return 1
		}
		ent := entries[e*12 : e*12+12]
		if order.Uint16(ent) != 0x0112 {
			continue
		}
		if order.Uint16(ent[2:]) != 3 || order.Uint32(ent[4:]) != 1 { // SHORT, count 1
			return 1
		}
		if v := int(order.Uint16(ent[8:])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}
