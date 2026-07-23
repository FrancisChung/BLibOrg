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
	"regexp"
	"strconv"
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

var pdfColorSpaceValueRe = regexp.MustCompile(`/ColorSpace\s*(/\w+|\[[^\]]*\]|\d+\s+\d+\s+R)`)
var pdfIndirectRefRe = regexp.MustCompile(`^(\d+)\s+\d+\s+R$`)
var pdfICCBasedRe = regexp.MustCompile(`(?s)\[\s*/ICCBased\s+(\d+)\s+\d+\s+R`)
var pdfStreamNRe = regexp.MustCompile(`/N\s+(\d+)`)

// resolvePDFColorSpaceValue returns the raw bytes describing an image's
// colorspace: a bare device name or an inline array is returned as-is; a
// name that isn't a recognized device colorspace (e.g. "/CS0") is looked
// up in resources' /ColorSpace subdictionary; an indirect reference ("N G
// R"), whether written directly or found via that lookup, is resolved
// through idx. ok is false if /ColorSpace is absent from dict or nothing
// above resolves it.
func resolvePDFColorSpaceValue(idx *pdfObjIndex, resources, dict []byte) (value []byte, ok bool) {
	m := pdfColorSpaceValueRe.FindSubmatch(dict)
	if m == nil {
		return nil, false
	}
	raw := m[1]

	if raw[0] == '/' {
		name := string(raw[1:])
		if _, isDevice := pdfDeviceColorSpaceByName[name]; isDevice {
			return raw, true
		}
		if resources == nil {
			return nil, false
		}
		csDict, ok := resolveDictValue(idx, resources, "ColorSpace")
		if !ok {
			return nil, false
		}
		entryRe := regexp.MustCompile(`/` + regexp.QuoteMeta(name) + `\s+(\[[^\]]*\]|\d+\s+\d+\s+R|/\w+)`)
		entry := entryRe.FindSubmatch(csDict)
		if entry == nil {
			return nil, false
		}
		raw = entry[1]
		if raw[0] == '/' {
			// A named resource pointing at another bare name (rare); resolve
			// once more against the device map only -- chasing further
			// indirection here is out of scope.
			if _, isDevice := pdfDeviceColorSpaceByName[string(raw[1:])]; isDevice {
				return raw, true
			}
			return nil, false
		}
	}

	if refMatch := pdfIndirectRefRe.FindSubmatch(raw); refMatch != nil {
		objNum, err := strconv.Atoi(string(refMatch[1]))
		if err != nil {
			return nil, false
		}
		body, ok := idx.lookup(objNum)
		if !ok {
			return nil, false
		}
		bodyDict, _, _ := splitPDFObjectBody(body)
		return bodyDict, true
	}
	return raw, true
}

// parsePDFColorSpace interprets colorspace bytes already resolved by
// resolvePDFColorSpaceValue: a bare device name maps directly; an
// ICCBased array is approximated by its stream dict's /N (component
// count) -- 1/3/4 -> Gray/RGB/CMYK, a pragmatic approximation rather than
// real ICC profile support (matching this package's convention, see the
// design doc). Indexed array support is added in Task 6.
func parsePDFColorSpace(idx *pdfObjIndex, raw []byte) (pdfColorSpace, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return pdfColorSpace{}, false
	}
	if trimmed[0] == '/' {
		cs, ok := pdfDeviceColorSpaceByName[string(trimmed[1:])]
		return cs, ok
	}
	if m := pdfICCBasedRe.FindSubmatch(trimmed); m != nil {
		objNum, err := strconv.Atoi(string(m[1]))
		if err != nil {
			return pdfColorSpace{}, false
		}
		body, ok := idx.lookup(objNum)
		if !ok {
			return pdfColorSpace{}, false
		}
		streamDict, _, _ := splitPDFObjectBody(body)
		nMatch := pdfStreamNRe.FindSubmatch(streamDict)
		if nMatch == nil {
			return pdfColorSpace{}, false
		}
		n, err := strconv.Atoi(string(nMatch[1]))
		if err != nil {
			return pdfColorSpace{}, false
		}
		switch n {
		case 1:
			return pdfDeviceGrayCS, true
		case 3:
			return pdfDeviceRGBCS, true
		case 4:
			return pdfDeviceCMYKCS, true
		default:
			return pdfColorSpace{}, false
		}
	}
	if bytes.Contains(trimmed, []byte("/Indexed")) {
		return parsePDFIndexedColorSpace(idx, trimmed)
	}
	return pdfColorSpace{}, false
}

var pdfIndexedBaseRe = regexp.MustCompile(`(?s)^\s*(/\w+|\[[^\]]*\])`)
var pdfIndexedHivalRe = regexp.MustCompile(`(?s)^(\d+)\s*(.*)$`)
var pdfIndexedStreamRefRe = regexp.MustCompile(`^(\d+)\s+\d+\s+R`)

// parsePDFIndexedColorSpace parses "[/Indexed base hival lookup]": base is
// another colorspace value (recursively parsed via parsePDFColorSpace),
// hival is the highest valid palette index, and lookup is either a
// literal PDF string "(...)" of raw palette bytes or a reference to a
// stream object containing them (both forms are real-world-common; a
// hex string "<...>" lookup table is not handled -- out of scope). The
// returned pdfColorSpace's toRGBA looks up base.components bytes at
// sample[0]*base.components within the palette, returning black for an
// out-of-range index rather than erroring, matching this package's
// best-effort convention.
func parsePDFIndexedColorSpace(idx *pdfObjIndex, arr []byte) (pdfColorSpace, bool) {
	inner := bytes.TrimSuffix(bytes.TrimPrefix(bytes.TrimSpace(arr), []byte("[")), []byte("]"))
	inner = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(inner), []byte("/Indexed")))

	baseMatch := pdfIndexedBaseRe.FindSubmatch(inner)
	if baseMatch == nil {
		return pdfColorSpace{}, false
	}
	base, ok := parsePDFColorSpace(idx, baseMatch[1])
	if !ok {
		return pdfColorSpace{}, false
	}
	rest := bytes.TrimSpace(inner[len(baseMatch[0]):])

	hivalMatch := pdfIndexedHivalRe.FindSubmatch(rest)
	if hivalMatch == nil {
		return pdfColorSpace{}, false
	}
	rest = bytes.TrimSpace(hivalMatch[2])

	var palette []byte
	switch {
	case len(rest) > 0 && rest[0] == '(':
		end := bytes.LastIndexByte(rest, ')')
		if end < 0 {
			return pdfColorSpace{}, false
		}
		palette = unescapePDFBytes(string(rest[1:end]))
	case pdfIndexedStreamRefRe.Match(rest):
		refMatch := pdfIndexedStreamRefRe.FindSubmatch(rest)
		objNum, err := strconv.Atoi(string(refMatch[1]))
		if err != nil {
			return pdfColorSpace{}, false
		}
		body, ok := idx.lookup(objNum)
		if !ok {
			return pdfColorSpace{}, false
		}
		_, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream {
			return pdfColorSpace{}, false
		}
		// splitPDFObjectBody may leave "endstream" in the stream if there's a
		// trailing newline before it, so we trim it here and then any remaining
		// whitespace (see decodePDFImageStream in pdf_images.go for the same
		// workaround).
		trimmed := bytes.TrimSuffix(stream, []byte("endstream"))
		trimmed = bytes.TrimRight(trimmed, "\r\n")
		palette = trimmed
	default:
		return pdfColorSpace{}, false
	}

	return pdfColorSpace{
		components: 1,
		toRGBA: func(s []byte) color.RGBA {
			off := int(s[0]) * base.components
			if off+base.components > len(palette) {
				return color.RGBA{A: 0xFF}
			}
			return base.toRGBA(palette[off : off+base.components])
		},
	}, true
}
