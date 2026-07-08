package metadata

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

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
