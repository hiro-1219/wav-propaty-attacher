package metadata

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestRenderID3v23(t *testing.T) {
	tag, err := RenderID3v23(Tag{Title: "曲名", Artist: "Artist", Album: "Album", Track: "1/2", Picture: Picture{MIME: "image/jpeg", Data: []byte{0xff, 0xd8, 0xff, 0xd9}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(tag[:3]) != "ID3" || tag[3] != 3 {
		t.Fatalf("unexpected header: %x", tag[:4])
	}
	size := int(tag[6])<<21 | int(tag[7])<<14 | int(tag[8])<<7 | int(tag[9])
	if size != len(tag)-10 {
		t.Fatalf("synchsafe size=%d, payload=%d", size, len(tag)-10)
	}
	for _, frame := range []string{"TIT2", "TPE1", "TALB", "TRCK", "APIC"} {
		if !bytes.Contains(tag, []byte(frame)) {
			t.Errorf("missing %s", frame)
		}
	}
	pos := bytes.Index(tag, []byte("TIT2"))
	frameSize := binary.BigEndian.Uint32(tag[pos+4 : pos+8])
	if frameSize < 5 {
		t.Fatalf("title frame too small: %d", frameSize)
	}
}

func TestRenderID3RejectsUnsupportedArtwork(t *testing.T) {
	_, err := RenderID3v23(Tag{Picture: Picture{MIME: "image/gif", Data: []byte("GIF89a")}})
	if err == nil {
		t.Fatal("expected error")
	}
}
