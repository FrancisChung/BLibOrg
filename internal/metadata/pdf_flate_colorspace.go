// This file resolves a PDF image XObject's colorspace and maps its
// predictor-reconstructed sample bytes to RGB, so pdf_flate.go's
// FlateDecode reconstruction can hand the frontend webview a real PNG.
// Indirect/named colorspace resolution (via Resources/ColorSpace) and
// ICCBased/Indexed support are added in later tasks; this task covers
// only the three Device* colorspaces, which alone account for the large
// majority of library covers per the design doc's sampling.
package metadata

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// pdfColorSpace describes how to map one pixel's raw sample bytes
// (already predictor-reconstructed, 8 bits per component) to an RGBA
// color. components is the number of sample bytes per pixel this
// colorspace consumes.
type pdfColorSpace struct {
	components int
	toRGBA     func(samples []byte) color.RGBA
}

var pdfDeviceGrayCS = pdfColorSpace{components: 1, toRGBA: func(s []byte) color.RGBA {
	return color.RGBA{R: s[0], G: s[0], B: s[0], A: 0xFF}
}}

var pdfDeviceRGBCS = pdfColorSpace{components: 3, toRGBA: func(s []byte) color.RGBA {
	return color.RGBA{R: s[0], G: s[1], B: s[2], A: 0xFF}
}}

// pdfDeviceCMYKCS converts via the standard naive CMYK->RGB formula
// (R=255*(1-C)*(1-K), etc.) -- not color-managed, but correct for the
// common case of a cover image with no embedded ICC profile.
var pdfDeviceCMYKCS = pdfColorSpace{components: 4, toRGBA: func(s []byte) color.RGBA {
	c, m, y, k := float64(s[0])/255, float64(s[1])/255, float64(s[2])/255, float64(s[3])/255
	r := 255 * (1 - c) * (1 - k)
	g := 255 * (1 - m) * (1 - k)
	b := 255 * (1 - y) * (1 - k)
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xFF}
}}

// pdfDeviceColorSpaceByName maps a PDF colorspace name to its pdfColorSpace.
// CalGray/CalRGB (calibrated variants) are treated identically to their
// Device counterparts -- this package doesn't attempt real color
// calibration, matching its "pragmatic approximation" convention.
var pdfDeviceColorSpaceByName = map[string]pdfColorSpace{
	"DeviceGray": pdfDeviceGrayCS,
	"DeviceRGB":  pdfDeviceRGBCS,
	"DeviceCMYK": pdfDeviceCMYKCS,
	"CalGray":    pdfDeviceGrayCS,
	"CalRGB":     pdfDeviceRGBCS,
}

// buildRGBAFromSamples maps predictor-reconstructed sample bytes
// (row-major, geo.width*geo.height pixels, cs.components bytes per pixel)
// into an image.RGBA and PNG-encodes it. ok is false if samples is
// shorter than the image's dimensions require (a truncated/malformed
// stream).
func buildRGBAFromSamples(samples []byte, geo pdfImageGeometry, cs pdfColorSpace) ([]byte, bool) {
	rowBytes := geo.width * cs.components
	if rowBytes <= 0 || len(samples) < rowBytes*geo.height {
		return nil, false
	}
	img := image.NewRGBA(image.Rect(0, 0, geo.width, geo.height))
	for y := 0; y < geo.height; y++ {
		row := samples[y*rowBytes : (y+1)*rowBytes]
		for x := 0; x < geo.width; x++ {
			px := row[x*cs.components : (x+1)*cs.components]
			img.SetRGBA(x, y, cs.toRGBA(px))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}
