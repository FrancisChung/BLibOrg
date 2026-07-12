package metadata

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// writeMobiFixture builds a minimal valid PalmDB+MOBI+EXTH file for testing.
func writeMobiFixture(t *testing.T, fullName, author, subject, pubdate string) string {
	t.Helper()
	buf := new(bytes.Buffer)

	// PalmDB header (78 bytes)
	name := make([]byte, 32)
	copy(name, "testbook")
	buf.Write(name)
	binary.Write(buf, binary.BigEndian, uint16(0)) // attributes
	binary.Write(buf, binary.BigEndian, uint16(0)) // version
	binary.Write(buf, binary.BigEndian, uint32(0)) // creation date
	binary.Write(buf, binary.BigEndian, uint32(0)) // mod date
	binary.Write(buf, binary.BigEndian, uint32(0)) // last backup
	binary.Write(buf, binary.BigEndian, uint32(0)) // mod number
	binary.Write(buf, binary.BigEndian, uint32(0)) // appInfoID
	binary.Write(buf, binary.BigEndian, uint32(0)) // sortInfoID
	buf.WriteString("BOOK")
	buf.WriteString("MOBI")
	binary.Write(buf, binary.BigEndian, uint32(0)) // uniqueIDseed
	binary.Write(buf, binary.BigEndian, uint32(0)) // nextRecordListID
	binary.Write(buf, binary.BigEndian, uint16(1)) // numRecords = 1

	// record info list: 1 entry, 8 bytes; record0 starts right after this entry
	record0Offset := uint32(78 + 8)
	binary.Write(buf, binary.BigEndian, record0Offset)
	binary.Write(buf, binary.BigEndian, uint32(0)) // attributes+uniqueID packed

	// record 0: PalmDOC header (16 bytes) + MOBI header + EXTH
	rec0 := new(bytes.Buffer)
	binary.Write(rec0, binary.BigEndian, uint16(1)) // compression
	binary.Write(rec0, binary.BigEndian, uint16(0)) // unused
	binary.Write(rec0, binary.BigEndian, uint32(0)) // text length
	binary.Write(rec0, binary.BigEndian, uint16(0)) // record count
	binary.Write(rec0, binary.BigEndian, uint16(0)) // record size
	binary.Write(rec0, binary.BigEndian, uint16(0)) // encryption type
	binary.Write(rec0, binary.BigEndian, uint16(0)) // unused2

	mobiHeaderStart := rec0.Len()
	const mobiHeaderLen = 232
	rec0.WriteString("MOBI")
	binary.Write(rec0, binary.BigEndian, uint32(mobiHeaderLen))
	binary.Write(rec0, binary.BigEndian, uint32(2))     // mobi type
	binary.Write(rec0, binary.BigEndian, uint32(65001)) // text encoding UTF-8
	binary.Write(rec0, binary.BigEndian, uint32(0))     // unique ID
	binary.Write(rec0, binary.BigEndian, uint32(6))     // file version

	for rec0.Len()-mobiHeaderStart < 84 {
		rec0.WriteByte(0)
	}
	binary.Write(rec0, binary.BigEndian, uint32(0x40)) // EXTH flags: bit6 set

	for rec0.Len()-mobiHeaderStart < 96 {
		rec0.WriteByte(0)
	}
	fullNameOffsetPos := rec0.Len()
	binary.Write(rec0, binary.BigEndian, uint32(0)) // placeholder full name offset
	binary.Write(rec0, binary.BigEndian, uint32(0)) // placeholder full name length

	for rec0.Len()-mobiHeaderStart < mobiHeaderLen {
		rec0.WriteByte(0)
	}

	// EXTH header
	type exthRecord struct {
		id   uint32
		data []byte
	}
	records := []exthRecord{
		{100, []byte(author)},
		{105, []byte(subject)},
		{106, []byte(pubdate)},
		{503, []byte(fullName)},
	}
	exthBody := new(bytes.Buffer)
	for _, r := range records {
		recLen := uint32(8 + len(r.data))
		binary.Write(exthBody, binary.BigEndian, r.id)
		binary.Write(exthBody, binary.BigEndian, recLen)
		exthBody.Write(r.data)
	}
	exthHeaderLen := uint32(12 + exthBody.Len())
	exthStart := rec0.Len()
	rec0.WriteString("EXTH")
	binary.Write(rec0, binary.BigEndian, exthHeaderLen)
	binary.Write(rec0, binary.BigEndian, uint32(len(records)))
	rec0.Write(exthBody.Bytes())
	for (rec0.Len()-exthStart)%4 != 0 {
		rec0.WriteByte(0)
	}

	fullNameOffset := uint32(rec0.Len())
	rec0.WriteString(fullName)

	out := rec0.Bytes()
	binary.BigEndian.PutUint32(out[fullNameOffsetPos:], fullNameOffset)
	binary.BigEndian.PutUint32(out[fullNameOffsetPos+4:], uint32(len(fullName)))
	buf.Write(out)

	dir := t.TempDir()
	path := filepath.Join(dir, "book.mobi")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write mobi fixture: %v", err)
	}
	return path
}

func TestExtractMobi(t *testing.T) {
	path := writeMobiFixture(t, "Foundation", "Isaac Asimov", "Sci-Fi", "1951-01-01")

	result, err := extractMobi(path)
	if err != nil {
		t.Fatalf("extractMobi returned error: %v", err)
	}
	if result.Title != "Foundation" {
		t.Errorf("Title = %q, want Foundation", result.Title)
	}
	if result.Author != "Isaac Asimov" {
		t.Errorf("Author = %q, want Isaac Asimov", result.Author)
	}
	if result.Subject != "Sci-Fi" {
		t.Errorf("Subject = %q, want Sci-Fi", result.Subject)
	}
	if result.Year != "1951" {
		t.Errorf("Year = %q, want 1951", result.Year)
	}
}

func TestExtractMobi_TooShort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.mobi")
	if err := os.WriteFile(path, []byte("short"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := extractMobi(path); err == nil {
		t.Error("expected error for too-short file, got nil")
	}
}
