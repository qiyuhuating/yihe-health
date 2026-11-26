package imagecodec

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"image/png"
	"math/bits"
)

// Sniff 按内容嗅探格式并解码像素（jpeg/png 用标准库，bmp 手工解析）。
func Sniff(data []byte) (format string, img image.Image, width, height int, ok bool) {
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil || !ValidDims(cfg.Width, cfg.Height) {
			return "", nil, 0, 0, false
		}
		m, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return "", nil, 0, 0, false
		}
		return "jpeg", m, cfg.Width, cfg.Height, true
	}
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' &&
		data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A {
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil || !ValidDims(cfg.Width, cfg.Height) {
			return "", nil, 0, 0, false
		}
		m, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return "", nil, 0, 0, false
		}
		return "png", m, cfg.Width, cfg.Height, true
	}
	if len(data) >= 2 && data[0] == 'B' && data[1] == 'M' {
		m, w, h, ok := decodeBMP(data)
		if !ok {
			return "", nil, 0, 0, false
		}
		return "bmp", m, w, h, true
	}
	return "", nil, 0, 0, false
}

func ValidDims(w, h int) bool {
	return w >= 1 && h >= 1 && w <= 8192 && h <= 8192 && int64(w)*int64(h) <= 12000000
}

// decodeBMP 手工解析 BMP（24 位 / 32 位无压缩，32 位 BI_BITFIELDS），返回 RGBA 像素。
func decodeBMP(data []byte) (image.Image, int, int, bool) {
	if len(data) < 54 {
		return nil, 0, 0, false
	}
	off := binary.LittleEndian.Uint32(data[10:14])
	dib := binary.LittleEndian.Uint32(data[14:18])
	if dib < 40 {
		return nil, 0, 0, false
	}
	w := int(int32(binary.LittleEndian.Uint32(data[18:22])))
	rawH := int32(binary.LittleEndian.Uint32(data[22:26]))
	planes := binary.LittleEndian.Uint16(data[26:28])
	bpp := binary.LittleEndian.Uint16(data[28:30])
	comp := binary.LittleEndian.Uint32(data[30:34])
	if planes != 1 || w < 1 || w > 8192 {
		return nil, 0, 0, false
	}
	topDown := false
	h := int(rawH)
	if rawH < 0 {
		h = int(-rawH)
		topDown = true
	}
	if h < 1 || h > 8192 || int64(w)*int64(h) > 12000000 {
		return nil, 0, 0, false
	}
	var rMask, gMask, bMask uint32 = 0x00FF0000, 0x0000FF00, 0x000000FF
	if comp == 3 { // BI_BITFIELDS
		if dib < 52 || len(data) < 66 {
			return nil, 0, 0, false
		}
		rMask = binary.LittleEndian.Uint32(data[54:58])
		gMask = binary.LittleEndian.Uint32(data[58:62])
		bMask = binary.LittleEndian.Uint32(data[62:66])
	} else if comp != 0 {
		return nil, 0, 0, false // 不支持 RLE 等压缩
	}
	if bpp != 24 && bpp != 32 {
		return nil, 0, 0, false
	}
	stride := (w*int(bpp)/8 + 3) &^ 3
	if int64(off)+int64(stride)*int64(h) > int64(len(data)) {
		return nil, 0, 0, false
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if unpackBMPNative(img.Pix, data[int(off):], w, h, stride, int(bpp)/8, topDown, rMask, gMask, bMask) {
		return img, w, h, true
	}
	for y := 0; y < h; y++ {
		sy := y
		if !topDown {
			sy = h - 1 - y
		}
		row := data[int(off)+sy*stride:][:w*int(bpp)/8]
		out := img.Pix[y*img.Stride:][:w*4]
		switch {
		case bpp == 24:
			unpackBGR24(out, row)
		case rMask == 0xff0000 && gMask == 0xff00 && bMask == 0xff:
			unpackBGRX32(out, row)
		default:
			for len(row) != 0 {
				px := binary.LittleEndian.Uint32(row)
				binary.LittleEndian.PutUint32(out, uint32(scaleMask(px, rMask))|uint32(scaleMask(px, gMask))<<8|uint32(scaleMask(px, bMask))<<16|0xff000000)
				row, out = row[4:], out[4:]
			}
		}
	}
	return img, w, h, true
}

// scaleMask 把掩码抽出的分量缩放到 8 位。
func scaleMask(px, mask uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := bits.TrailingZeros32(mask)
	width := bits.OnesCount32(mask)
	v := (px & mask) >> shift
	if width >= 8 {
		return uint8(v >> (width - 8))
	}
	return uint8(v * 255 / ((1 << width) - 1))
}
