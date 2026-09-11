package audio

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"wav-property-attacher/internal/metadata"
)

func TestReadAndExportAIFFPreservesSamples(t *testing.T) {
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "01.wav")
	pcm := make([]byte, 441*2*2)
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:i+2], uint16(i*17))
	}
	writeTestWAV(t, wavPath, 2, 44100, 16, pcm)
	info, err := ReadWAV(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(pcm)
	if info.AudioHash != hex.EncodeToString(want[:]) {
		t.Fatalf("audio hash mismatch")
	}
	if info.Format.Frames != 441 {
		t.Fatalf("frames=%d", info.Format.Frames)
	}
	aiffPath := filepath.Join(dir, "rekordbox", "01.aiff")
	hash, err := ExportAIFF(wavPath, aiffPath, metadata.Tag{Title: "テスト", Artist: "Artist", Album: "Album", Track: "1/1"})
	if err != nil {
		t.Fatal(err)
	}
	if hash != info.AudioHash {
		t.Fatalf("output hash=%s input=%s", hash, info.AudioHash)
	}
	data, err := os.ReadFile(aiffPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range [][]byte{[]byte("ID3 "), []byte("TIT2"), []byte("TPE1"), []byte("TALB")} {
		if !bytes.Contains(data, marker) {
			t.Fatalf("missing AIFF/ID3 marker %q", marker)
		}
	}
}

func TestRejectsFloatAndUnsupportedDepth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.wav")
	writeTestWAVWithCode(t, path, 3, 2, 44100, 32, make([]byte, 16))
	if _, err := ReadWAV(path); err == nil {
		t.Fatal("expected float WAV rejection")
	}
}

func TestScanAlbumDiscFolders(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Disc 2"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestWAV(t, filepath.Join(dir, "02.wav"), 2, 44100, 16, make([]byte, 40))
	writeTestWAV(t, filepath.Join(dir, "01.wav"), 2, 44100, 16, make([]byte, 40))
	writeTestWAV(t, filepath.Join(dir, "Disc 2", "01.wav"), 2, 44100, 16, make([]byte, 40))
	tracks, err := ScanAlbum(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 3 {
		t.Fatalf("tracks=%d", len(tracks))
	}
	if tracks[0].TrackHint != 1 || tracks[1].TrackHint != 2 || tracks[2].DiscHint != 2 {
		t.Fatalf("unexpected order: %+v", tracks)
	}
}

func writeTestWAV(t *testing.T, path string, channels uint16, rate uint32, bits uint16, pcm []byte) {
	t.Helper()
	writeTestWAVWithCode(t, path, 1, channels, rate, bits, pcm)
}
func writeTestWAVWithCode(t *testing.T, path string, code, channels uint16, rate uint32, bits uint16, pcm []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(4+8+16+8+len(pcm)))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, code)
	_ = binary.Write(&b, binary.LittleEndian, channels)
	_ = binary.Write(&b, binary.LittleEndian, rate)
	align := channels * (bits / 8)
	_ = binary.Write(&b, binary.LittleEndian, rate*uint32(align))
	_ = binary.Write(&b, binary.LittleEndian, align)
	_ = binary.Write(&b, binary.LittleEndian, bits)
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(pcm)))
	b.Write(pcm)
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
