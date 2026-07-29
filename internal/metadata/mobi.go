package metadata

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"

	"github.com/FrancisChung/BLibOrg/internal/textutil"
)

var (
	jpegMagic  = []byte{0xFF, 0xD8, 0xFF}
	pngMagic   = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	gif87Magic = []byte("GIF87a")
	gif89Magic = []byte("GIF89a")
)

func sniffImageContentType(data []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(data, jpegMagic):
		return "image/jpeg", true
	case bytes.HasPrefix(data, pngMagic):
		return "image/png", true
	case bytes.HasPrefix(data, gif87Magic), bytes.HasPrefix(data, gif89Magic):
		return "image/gif", true
	default:
		return "", false
	}
}

// findMobiCover scans the PDB records after record 0 (the MOBI/EXTH header
// and text record) for the first one whose leading bytes match a recognized
// image signature (JPEG/PNG/GIF), and returns it as the cover. See this
// task's plan entry for why this replaces computing the exact record via
// EXTH 201 "Cover Offset" plus the MOBI header's "first image record"
// field.
func findMobiCover(data []byte, numRecords uint16) ([]byte, string, bool) {
	offsets := make([]uint32, numRecords)
	for i := uint16(0); i < numRecords; i++ {
		pos := 78 + int(i)*8
		if pos+4 > len(data) {
			return nil, "", false
		}
		offsets[i] = binary.BigEndian.Uint32(data[pos : pos+4])
	}
	for i := 1; i < int(numRecords); i++ {
		start := int(offsets[i])
		end := len(data)
		if i+1 < int(numRecords) {
			end = int(offsets[i+1])
		}
		if start < 0 || start >= end || end > len(data) {
			continue
		}
		if ct, ok := sniffImageContentType(data[start:end]); ok {
			return data[start:end], ct, true
		}
	}
	return nil, "", false
}

// extractMobi parses the PalmDB + MOBI header + EXTH structure shared by
// .mobi and .azw3 files. It is best-effort: on any structural surprise past
// the point where core fields have already been read, it returns whatever
// it has rather than erroring, so callers can still fall back to heuristics
// for missing fields.
func extractMobi(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	if len(data) < 82 {
		return Result{}, fmt.Errorf("file too short to be a valid MOBI/AZW3")
	}
	numRecords := binary.BigEndian.Uint16(data[76:78])
	if numRecords < 1 {
		return Result{}, fmt.Errorf("no records found")
	}
	record0Offset := binary.BigEndian.Uint32(data[78:82])
	if int(record0Offset) >= len(data) {
		return Result{}, fmt.Errorf("record0 offset out of range")
	}
	rec0 := data[record0Offset:]

	const mobiHeaderStart = 16
	if len(rec0) < mobiHeaderStart+104 {
		return Result{}, fmt.Errorf("record0 too short for MOBI header")
	}
	if string(rec0[mobiHeaderStart:mobiHeaderStart+4]) != "MOBI" {
		return Result{}, fmt.Errorf("MOBI identifier not found")
	}
	headerLen := binary.BigEndian.Uint32(rec0[mobiHeaderStart+4 : mobiHeaderStart+8])
	exthFlags := binary.BigEndian.Uint32(rec0[mobiHeaderStart+84 : mobiHeaderStart+88])
	fullNameOffset := binary.BigEndian.Uint32(rec0[mobiHeaderStart+96 : mobiHeaderStart+100])
	fullNameLen := binary.BigEndian.Uint32(rec0[mobiHeaderStart+100 : mobiHeaderStart+104])

	var result Result
	if uint64(fullNameOffset)+uint64(fullNameLen) <= uint64(len(rec0)) {
		result.Title = string(rec0[fullNameOffset : fullNameOffset+fullNameLen])
	}

	if coverData, coverContentType, ok := findMobiCover(data, numRecords); ok {
		result.CoverBytes = coverData
		result.CoverContentType = coverContentType
	}

	if exthFlags&0x40 == 0 {
		return result, nil // no EXTH block present
	}
	exthStart := mobiHeaderStart + int(headerLen)
	if exthStart+12 > len(rec0) || string(rec0[exthStart:exthStart+4]) != "EXTH" {
		return result, nil
	}
	recordCount := binary.BigEndian.Uint32(rec0[exthStart+8 : exthStart+12])
	pos := exthStart + 12
	var pubdate string
	for i := uint32(0); i < recordCount; i++ {
		if pos+8 > len(rec0) {
			break
		}
		recType := binary.BigEndian.Uint32(rec0[pos : pos+4])
		recLen := binary.BigEndian.Uint32(rec0[pos+4 : pos+8])
		if recLen < 8 || pos+int(recLen) > len(rec0) {
			break
		}
		recData := rec0[pos+8 : pos+int(recLen)]
		switch recType {
		case 100:
			result.Author = string(recData)
		case 105:
			result.Subject = string(recData)
		case 106:
			pubdate = string(recData)
		case 503:
			result.Title = string(recData) // updated title overrides PalmDOC full name
		}
		pos += int(recLen)
	}
	if pubdate != "" {
		if year, ok := textutil.ExtractYear(pubdate); ok {
			result.Year = year
		}
	}

	return result, nil
}
