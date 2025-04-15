// ◄◄◄ gobmp/reader.go ►►►
// Copyright © 2012 Jason Summers
// Use of this code is governed by an MIT-style license that can
// be found in the readme.md file.
//
// BMP file decoder
//

// Package gobmp implements a BMP image decoder and encoder.
package gobmp

import "image"
import "image/color"
import "io"
import "fmt"

const (
	bI_RGB       = 0
	bI_RLE8      = 1
	bI_RLE4      = 2
	bI_BITFIELDS = 3
)

type bitFieldsInfo struct {
	mask  uint32
	shift uint
	scale float64 // Amount to multiply the sample value by, to scale it to [0..255]
}

type decoder struct {
	r io.Reader

	img_Paletted *image.Paletted // Used if dstHasPalette is true
	img_NRGBA    *image.NRGBA    // Used otherwise

	bfOffBits  uint32
	headerSize uint32
	width         int
	height        int
	bitCount      int
	biCompression uint32
	isTopDown     bool

	srcPalNumEntries    int
	srcPalBytesPerEntry int
	srcPalSizeInBytes   int
	dstPalNumEntries    int
	dstHasPalette       bool
	dstPalette          color.Palette

	hasBitFieldsSegment  bool
	bitFieldsSegmentSize int
	bitFields            [4]bitFieldsInfo
	memoryLimit          uint32
}

var ErrMemoryLimitExceeded = UnsupportedError("memory limit exceeded during BMP decoding")

type DecodeOptions struct {
	// max bytes allowed to allocate
	memoryLimit uint32
}

type DecodeOptFunc func(*DecodeOptions)

// WithMemoryLimit sets memory limit
func WithMemoryLimit(limit uint32) DecodeOptFunc {
	return func(o *DecodeOptions) {
		o.memoryLimit = max(o.memoryLimit, limit)
	}
}

// An UnsupportedError reports that the input uses a valid but unimplemented
// BMP feature.
type UnsupportedError string

func (e UnsupportedError) Error() string { return "bmp: unsupported feature: " + string(e) }

// A FormatError reports that the input is not a valid BMP file.
type FormatError string

func (e FormatError) Error() string { return "bmp: invalid format: " + string(e) }

// safety: these 2 functions take in a slice
// with length equal to the number bytes they access
func getWORD(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8
}
func getDWORD(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func decodeRow_paletted(d *decoder, buf []byte, row int) error {
	rowSize := d.img_Paletted.Stride
	for col := 0; col < d.width; col++ {
		var colorInd byte

		switch d.bitCount {
		case 8:
			colorInd = buf[col]
		case 4:
			colorInd = (buf[col/2] >> (4 * (1 - uint(col)%2))) & 0x0f
		case 2:
			colorInd = (buf[col/4] >> (2 * (3 - uint(col)%4))) & 0x03
		case 1:
			colorInd = (buf[col/8] >> (1 * (7 - uint(col)%8))) & 0x01
		}
		if int(colorInd) >= d.dstPalNumEntries {
			// Out-of-range palette index.
			// Most BMP viewers use the first palette color for such pixels, so
			// that's what we'll do.
			colorInd = 0
		}
		d.img_Paletted.Pix[row*rowSize+col] = colorInd
	}
	return nil
}

func decodeRow_16or32(d *decoder, buf []byte, j int) error {
	for i := 0; i < d.width; i++ {
		var v uint32
		if d.bitCount == 16 {
			v = uint32(getWORD(buf[i*2 : i*2+2]))
		} else { // bitCount == 32
			v = getDWORD(buf[i*4 : i*4+4])
		}
		for k := 0; k < 4; k++ {
			var sv uint8
			if d.bitFields[k].mask == 0 {
				if k == 3 {
					// If alpha mask is missing, make the pixel opaque.
					sv = 255
				} else {
					// If some other mask is missing, who knows what to do?
					sv = 0
				}
			} else {
				sv = uint8(0.5 + float64((v&d.bitFields[k].mask)>>d.bitFields[k].shift)*
					d.bitFields[k].scale)
			}
			d.img_NRGBA.Pix[j*d.img_NRGBA.Stride+i*4+k] = sv
		}
	}
	return nil
}

func decodeRow_24(d *decoder, buf []byte, j int) error {
	for i := 0; i < d.width; i++ {
		for k := 0; k < 3; k++ {
			d.img_NRGBA.Pix[j*d.img_NRGBA.Stride+i*4+k] = buf[i*3+2-k]
		}
		d.img_NRGBA.Pix[j*d.img_NRGBA.Stride+i*4+3] = 255
	}
	return nil
}

type decodeRowFuncType func(d *decoder, buf []byte, j int) error

var rowDecoders = map[int]decodeRowFuncType{
	1:  decodeRow_paletted,
	2:  decodeRow_paletted,
	4:  decodeRow_paletted,
	8:  decodeRow_paletted,
	16: decodeRow_16or32,
	24: decodeRow_24,
	32: decodeRow_16or32,
}

func (d *decoder) readBitsUncompressed() error {

	srcRowStride := ((d.width*d.bitCount + 31) / 32) * 4
	// d.width limited before this
	buf := make([]byte, srcRowStride)

	decodeRowFunc := rowDecoders[d.bitCount]
	if decodeRowFunc == nil {
		return nil
	}

	for srcRow := 0; srcRow < d.height; srcRow++ {
		var dstRow int

		if d.isTopDown {
			dstRow = srcRow
		} else {
			dstRow = d.height - srcRow - 1
		}

		_, err := io.ReadFull(d.r, buf)
		if err != nil {
			return err
		}
		err = decodeRowFunc(d, buf, dstRow)
		if err != nil {
			return err
		}
	}
	return nil
}

// If there is a gap before the bits, skip over it.
func (d *decoder) readGap() error {
	currentOffset := 14 + int(d.headerSize) + d.bitFieldsSegmentSize + d.srcPalSizeInBytes
	if currentOffset == int(d.bfOffBits) {
		return nil
	}
	if currentOffset > int(d.bfOffBits) {
		return FormatError("bad bfOffBits field")
	}
	gapSize := int(d.bfOffBits) - currentOffset

	_, err := io.CopyN(io.Discard, d.r, int64(gapSize))
	return err
}

// Read a 12-byte BITMAPCOREHEADER.
func decodeInfoHeader12(d *decoder, h []byte, configOnly bool) error {
	d.width = int(getWORD(h[4:6]))
	d.height = int(getWORD(h[6:8]))
	d.bitCount = int(getWORD(h[10:12]))
	d.srcPalBytesPerEntry = 3
	if d.bitCount >= 1 && d.bitCount <= 8 {
		d.srcPalNumEntries = 1 << uint(d.bitCount)
	}

	// Figure out how many palette entries there are.
	// A full-size palette is expected, but if the palette is overlapped by the
	// bitmap, we assume the palette is less than full size.
	paletteStart := 14 + int(d.headerSize)
	paletteEnd := paletteStart + d.srcPalBytesPerEntry*d.srcPalNumEntries
	if int(d.bfOffBits) >= paletteStart+d.srcPalBytesPerEntry && int(d.bfOffBits) < paletteEnd {
		d.srcPalNumEntries = (int(d.bfOffBits) - paletteStart) / 3
	}

	return nil
}

// Read a 40-byte BITMAPINFOHEADER.
// Use of this function does not imply that the entire header is 40 bytes.
// We may just be decoding the first 40 bytes of a 108- or 124-byte header.
func decodeInfoHeader40(d *decoder, h []byte, configOnly bool) error {
	d.width = int(getDWORD(h[4:8]))
	d.height = int(getDWORD(h[8:12]))
	if d.height < 0 {
		d.isTopDown = true
		d.height = -d.height
	}
	d.bitCount = int(getWORD(h[14:16]))
	if configOnly {
		return nil
	}

	if len(h) >= 20 {
		d.biCompression = getDWORD(h[16:20])
	}

	if d.biCompression == bI_BITFIELDS && d.headerSize == 40 && d.bitCount != 1 {
		d.hasBitFieldsSegment = true
		d.bitFieldsSegmentSize = 12
	}

	if d.biCompression == bI_RGB {
		if d.bitCount == 16 {
			// Default bitfields for 16-bit images:
			d.recordBitFields(0x7c00, 0x03e0, 0x001f, 0)
		} else if d.bitCount == 32 {
			// Default bitfields for 32-bit images:
			d.recordBitFields(0x00ff0000, 0x0000ff00, 0x000000ff, 0)
		}
	}

	var biClrUsed uint32
	if len(h) >= 36 {
		biClrUsed = getDWORD(h[32:36])
	}
	if biClrUsed > 10000 {
		return FormatError(fmt.Sprintf("bad palette size %d", biClrUsed))
	}

	// Figure out how many colors (that we care about) are in the palette.
	if d.bitCount >= 1 && d.bitCount <= 8 {
		if biClrUsed == 0 || biClrUsed > (1<<uint(d.bitCount)) {
			d.srcPalNumEntries = 1 << uint(d.bitCount)
		} else {
			d.srcPalNumEntries = int(biClrUsed)
		}
	} else {
		d.srcPalNumEntries = 0
	}

	d.srcPalBytesPerEntry = 4

	if d.headerSize == 64 && d.srcPalNumEntries > 0 {
		// A hack to allow (invalid?) OS/2v2 BMPs that have 3 bytes per palette
		// entry instead of 4.
		if 14+d.headerSize+uint32(d.bitFieldsSegmentSize)+3*uint32(d.srcPalNumEntries) ==
			d.bfOffBits {
			d.srcPalBytesPerEntry = 3
		}
	}

	return nil
}

func decodeInfoHeader108(d *decoder, h []byte, configOnly bool) error {
	err := decodeInfoHeader40(d, h[:40], configOnly)
	if err != nil {
		return err
	}

	if d.biCompression == bI_BITFIELDS {
		var bf_alpha uint32
		if len(h) >= 56 {
			bf_alpha = getDWORD(h[52:56])
		}
		d.recordBitFields(getDWORD(h[40:44]), getDWORD(h[44:48]),
			getDWORD(h[48:52]), bf_alpha)
	}
	return nil
}

type decodeInfoHeaderFuncType func(d *decoder, h []byte, configOnly bool) error

func readInfoHeader(d *decoder, configOnly bool) error {
	var h []byte
	var decodeFn decodeInfoHeaderFuncType

	switch d.headerSize {
	case 12:
		decodeFn = decodeInfoHeader12
	case 16, 20, 24, 32, 36, 40, 42, 44, 46, 48, 60, 64:
		decodeFn = decodeInfoHeader40
	case 52, 56, 108, 124:
		decodeFn = decodeInfoHeader108
	default:
		return UnsupportedError(fmt.Sprintf("BMP version (header size %d)", d.headerSize))
	}

	// Read the rest of the infoheader
	h = make([]byte, d.headerSize)
	_, err := io.ReadFull(d.r, h[4:])
	if err != nil {
		return err
	}

	err = decodeFn(d, h, configOnly)
	if err != nil {
		return err
	}
	if d.width < 1 {
		return FormatError(fmt.Sprintf("bad width %d", d.width))
	}
	if d.height < 1 {
		return FormatError(fmt.Sprintf("bad height %d", d.height))
	}

	if d.bitCount >= 1 && d.bitCount <= 8 {
		d.dstHasPalette = true
	}

	d.srcPalSizeInBytes = d.srcPalNumEntries * d.srcPalBytesPerEntry

	return nil
}

func (d *decoder) recordBitFields(r, g, b, a uint32) {
	d.bitFields[0].mask = r
	d.bitFields[1].mask = g
	d.bitFields[2].mask = b
	d.bitFields[3].mask = a

	// Based on .mask, set the other fields of the bitFields struct
	for k := 0; k < 4; k++ {
		if d.bitFields[k].mask == 0 {
			continue
		}

		// Starting with the low bit, count the number of 0 bits before
		// the first 1 bit.
		tmpMask := d.bitFields[k].mask
		for tmpMask&0x1 == 0 {
			d.bitFields[k].shift++
			tmpMask >>= 1
		}
		d.bitFields[k].scale = 255.0 / float64(tmpMask)
	}
}

func (d *decoder) readBitFieldsSegment() error {
	// 12 or 0
	buf := make([]byte, d.bitFieldsSegmentSize)
	_, err := io.ReadFull(d.r, buf[:])
	if err != nil {
		return err
	}
	d.recordBitFields(getDWORD(buf[0:4]), getDWORD(buf[4:8]),
		getDWORD(buf[8:12]), 0)
	return nil
}

func (d *decoder) readPalette() error {
	buf := make([]byte, d.srcPalSizeInBytes)
	_, err := io.ReadFull(d.r, buf)
	if err != nil {
		return err
	}

	if !d.dstHasPalette {
		d.dstPalNumEntries = 0
		return nil
	}

	d.dstPalNumEntries = d.srcPalNumEntries
	if d.dstPalNumEntries > 256 {
		d.dstPalNumEntries = 256
	}

	d.dstPalette = make(color.Palette, d.dstPalNumEntries)
	for i := 0; i < d.dstPalNumEntries; i++ {
		// TODO: find 4 byte entry palette bmp
		// and add branch to handle this (simple)
		d.dstPalette[i] = color.RGBA{buf[i*d.srcPalBytesPerEntry+2],
			buf[i*d.srcPalBytesPerEntry+1],
			buf[i*d.srcPalBytesPerEntry+0], 255}
	}
	return nil
}

func (d *decoder) decodeFileHeader(b []byte) error {
	if b[0] != 0x42 || b[1] != 0x4d {
		return FormatError("not a BMP file")
	}
	d.bfOffBits = getDWORD(b[10:14])
	return nil
}

func (d *decoder) readHeaders(configOnly bool) error {
	var fh [18]byte

	// Read the file header, and the first 4 bytes of the info header
	_, err := io.ReadFull(d.r, fh[:])
	if err != nil {
		return err
	}

	err = d.decodeFileHeader(fh[0:14])
	if err != nil {
		return err
	}

	d.headerSize = getDWORD(fh[14:18])

	err = readInfoHeader(d, configOnly)
	if err != nil {
		return err
	}

	return nil
}

func (d *decoder) readMain(r io.Reader, configOnly bool) (image.Image, error) {

	// Read the FILEHEADER and INFOHEADER.
	err := d.readHeaders(false)
	if err != nil {
		return nil, err
	}

	// Make sure bitcount and "compression" are valid and compatible.
	switch d.biCompression {
	case bI_RGB:
		// We allow bitCount=2 because Windows CE defines it to be valid
		// (at least if headerSize=40).
		if d.bitCount != 1 && d.bitCount != 2 && d.bitCount != 4 && d.bitCount != 8 &&
			d.bitCount != 16 && d.bitCount != 24 && d.bitCount != 32 {
			return nil, FormatError(fmt.Sprintf("bad bit count %d", d.bitCount))
		}
	case bI_RLE4:
		if d.bitCount != 4 {
			return nil, FormatError(fmt.Sprintf("bad RLE4 bit count %d", d.bitCount))
		}
	case bI_RLE8:
		if d.bitCount != 8 {
			return nil, FormatError(fmt.Sprintf("bad RLE8 bit count %d", d.bitCount))
		}
	case bI_BITFIELDS:
		if d.bitCount == 1 {
			return nil, UnsupportedError("Huffman 1D compression")
		} else if d.bitCount != 16 && d.bitCount != 32 {
			return nil, FormatError(fmt.Sprintf("bad BITFIELDS bit count %d", d.bitCount))
		}
	default:
		return nil, UnsupportedError(fmt.Sprintf("compression or image type %d", d.biCompression))
	}

	// Read the BITFIELDS segment, if present.
	if d.hasBitFieldsSegment {
		err = d.readBitFieldsSegment()
		if err != nil {
			return nil, err
		}
	}

	// Read the palette, if present.
	if d.srcPalNumEntries > 0 {
		err = d.readPalette()
		if err != nil {
			return nil, err
		}
	}

	if configOnly {
		return nil, nil
	}

	// Create the target image.
	if d.dstHasPalette {
		if d.memoryLimit > 0 && uint32(d.width*d.height) > d.memoryLimit {
			return nil, ErrMemoryLimitExceeded
		}
		d.img_Paletted = image.NewPaletted(image.Rect(0, 0, d.width, d.height), d.dstPalette)
	} else {
		if d.memoryLimit > 0 && uint32(4*d.width*d.height) > d.memoryLimit {
			return nil, ErrMemoryLimitExceeded
		}
		d.img_NRGBA = image.NewNRGBA(image.Rect(0, 0, d.width, d.height))
	}

	// Skip over any unused space preceding the bitmap bits.
	err = d.readGap()
	if err != nil {
		return nil, err
	}

	// Read the bitmap bits.
	if d.biCompression == bI_RLE4 || d.biCompression == bI_RLE8 {
		err = d.readBitsRLE()
	} else {
		err = d.readBitsUncompressed()
	}
	if err != nil {
		return nil, err
	}

	if d.dstHasPalette {
		return d.img_Paletted, nil
	}
	return d.img_NRGBA, nil
}

// Decode reads a BMP image from r and returns it as an image.Image.
func Decode(r io.Reader, opts ...DecodeOptFunc) (image.Image, error) {

	d := new(decoder)
	d.r = r
	options := DecodeOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	d.memoryLimit = options.memoryLimit

	im, err := d.readMain(r, false)
	return im, err
}

// DecodeConfig returns the color model and dimensions of the BMP image without
// decoding the entire image.
func DecodeConfig(r io.Reader) (image.Config, error) {
	var cfg image.Config

	d := new(decoder)
	d.r = r

	_, err := d.readMain(r, true)
	if err != nil {
		return cfg, err
	}

	cfg.Width = d.width
	cfg.Height = d.height
	if d.dstHasPalette {
		cfg.ColorModel = d.dstPalette
	} else {
		cfg.ColorModel = color.NRGBAModel
	}

	return cfg, nil
}

func init() {
	image.RegisterFormat("bmp", "BM", func(r io.Reader) (image.Image, error) {
		img, err := Decode(r)
		if err != nil {
			return nil, err
		}
		return img, nil
	}, DecodeConfig)
}
