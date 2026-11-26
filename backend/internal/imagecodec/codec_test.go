package imagecodec

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"image/png"
	"math/rand"
	"testing"
)

func newBMP(width, height int, bitsPerPixel uint16) []byte {
	stride := (width*int(bitsPerPixel)/8 + 3) &^ 3
	data := make([]byte, 54+stride*height)
	copy(data, "BM")
	binary.LittleEndian.PutUint32(data[10:], 54)
	binary.LittleEndian.PutUint32(data[14:], 40)
	binary.LittleEndian.PutUint32(data[18:], uint32(width))
	binary.LittleEndian.PutUint32(data[22:], uint32(height))
	binary.LittleEndian.PutUint16(data[26:], 1)
	binary.LittleEndian.PutUint16(data[28:], bitsPerPixel)
	return data
}

func encodedPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func encodedJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := jpeg.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestSniffRecognizesSupportedFormats(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		format string
	}{
		{"png", encodedPNG(t, 3, 2), "png"},
		{"jpeg", encodedJPEG(t, 3, 2), "jpeg"},
		{"bmp24", newBMP(3, 2, 24), "bmp"},
		{"bmp32", newBMP(3, 2, 32), "bmp"},
	} {
		format, img, width, height, ok := Sniff(tc.data)
		if !ok || format != tc.format || img == nil || width != 3 || height != 2 {
			t.Fatalf("%s: got (%q, %v, %d, %d, %v)", tc.name, format, img != nil, width, height, ok)
		}
	}
}

func TestSniffRejectsUnknownAndTruncatedInput(t *testing.T) {
	pngData := encodedPNG(t, 3, 2)
	jpegData := encodedJPEG(t, 3, 2)
	for name, data := range map[string][]byte{
		"nil":            nil,
		"empty":          {},
		"one byte":       {0xFF},
		"jpeg signature": {0xFF, 0xD8},
		"png signature":  pngData[:8],
		"truncated png":  pngData[:len(pngData)/2],
		"truncated jpeg": jpegData[:len(jpegData)/2],
		"bmp signature":  []byte("BM"),
		"gif":            []byte("GIF89a\x01\x00\x01\x00"),
		"text":           []byte("<html>not an image</html>"),
	} {
		if format, img, _, _, ok := Sniff(data); ok || img != nil || format != "" {
			t.Fatalf("%s was accepted as %q", name, format)
		}
	}
}

func TestValidDimsLimits(t *testing.T) {
	for _, tc := range []struct {
		width, height int
		ok            bool
	}{
		{1, 1, true},
		{8192, 1, true},
		{4000, 3000, true},
		{4001, 3000, false},
		{8192, 8192, false},
		{8193, 1, false},
		{1, 8193, false},
		{0, 1, false},
		{1, 0, false},
		{-1, 1, false},
	} {
		if got := ValidDims(tc.width, tc.height); got != tc.ok {
			t.Fatalf("ValidDims(%d, %d) = %v, want %v", tc.width, tc.height, got, tc.ok)
		}
	}
}

func TestDecodeBMPRejectsMalformedHeaders(t *testing.T) {
	for name, mutate := range map[string]func([]byte) []byte{
		"short header":         func(d []byte) []byte { return d[:53] },
		"planes not one":       func(d []byte) []byte { binary.LittleEndian.PutUint16(d[26:], 2); return d },
		"zero width":           func(d []byte) []byte { binary.LittleEndian.PutUint32(d[18:], 0); return d },
		"negative width":       func(d []byte) []byte { binary.LittleEndian.PutUint32(d[18:], 0xFFFFFFFF); return d },
		"width over limit":     func(d []byte) []byte { binary.LittleEndian.PutUint32(d[18:], 8193); return d },
		"zero height":          func(d []byte) []byte { binary.LittleEndian.PutUint32(d[22:], 0); return d },
		"minimum int32 height": func(d []byte) []byte { binary.LittleEndian.PutUint32(d[22:], 0x80000000); return d },
		"height over limit":    func(d []byte) []byte { binary.LittleEndian.PutUint32(d[22:], 8193); return d },
		"area over limit": func(d []byte) []byte {
			binary.LittleEndian.PutUint32(d[18:], 8192)
			binary.LittleEndian.PutUint32(d[22:], 8192)
			return d
		},
		"unsupported depth":      func(d []byte) []byte { binary.LittleEndian.PutUint16(d[28:], 16); return d },
		"rle compression":        func(d []byte) []byte { binary.LittleEndian.PutUint32(d[30:], 1); return d },
		"bitfields without mask": func(d []byte) []byte { binary.LittleEndian.PutUint32(d[30:], 3); return d },
		"dib header too small":   func(d []byte) []byte { binary.LittleEndian.PutUint32(d[14:], 39); return d },
		"offset at end of file":  func(d []byte) []byte { binary.LittleEndian.PutUint32(d[10:], uint32(len(d))); return d },
		"offset beyond file":     func(d []byte) []byte { binary.LittleEndian.PutUint32(d[10:], 0xFFFFFFFF); return d },
		"missing last pixel row": func(d []byte) []byte { return d[:len(d)-1] },
	} {
		data := mutate(newBMP(4, 4, 24))
		if _, _, _, ok := decodeBMP(data); ok {
			t.Fatalf("%s was accepted", name)
		}
		if _, img, _, _, ok := Sniff(data); ok || img != nil {
			t.Fatalf("%s was accepted by Sniff", name)
		}
	}
}

// A fixed seed keeps the run reproducible while exercising header fields the
// hand-written cases above do not enumerate, including the native unpack path.
func TestSniffSurvivesMutatedBMPHeaders(t *testing.T) {
	seeds := [][]byte{newBMP(5, 3, 24), newBMP(5, 3, 32)}
	bitfields := newBMP(5, 3, 32)
	bitfields = append(bitfields[:54:54], append(make([]byte, 12), bitfields[54:]...)...)
	binary.LittleEndian.PutUint32(bitfields[10:], 66)
	binary.LittleEndian.PutUint32(bitfields[14:], 52)
	binary.LittleEndian.PutUint32(bitfields[30:], 3)
	binary.LittleEndian.PutUint32(bitfields[54:], 0x00FF0000)
	binary.LittleEndian.PutUint32(bitfields[58:], 0x0000FF00)
	binary.LittleEndian.PutUint32(bitfields[62:], 0x000000FF)
	seeds = append(seeds, bitfields)

	const iterations = 30000
	accepted := 0
	rng := rand.New(rand.NewSource(20261109))
	for i := 0; i < iterations; i++ {
		data := append([]byte(nil), seeds[i%len(seeds)]...)
		for edits := rng.Intn(4) + 1; edits > 0; edits-- {
			position := rng.Intn(len(data))
			if rng.Intn(10) < 7 {
				position = rng.Intn(66)
			}
			data[position] = byte(rng.Intn(256))
		}
		if rng.Intn(4) == 0 {
			data = data[:rng.Intn(len(data)+1)]
		}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("Sniff panicked on iteration %d: %v (input % x)", i, recovered, data)
				}
			}()
			_, img, width, height, ok := Sniff(data)
			if !ok {
				return
			}
			accepted++
			if img == nil || !ValidDims(width, height) {
				t.Fatalf("iteration %d accepted an inconsistent image: %dx%d", i, width, height)
			}
		}()
	}
	// Guards against the loop silently rejecting everything and proving nothing.
	if accepted < iterations/10 || accepted == iterations {
		t.Fatalf("accepted %d of %d mutated inputs; mutation mix no longer exercises both paths", accepted, iterations)
	}
	t.Logf("accepted %d of %d mutated inputs", accepted, iterations)
}
