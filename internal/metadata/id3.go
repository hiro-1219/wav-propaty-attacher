package metadata

import (
	"bytes"
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

type Picture struct {
	MIME string
	Data []byte
}

type Tag struct {
	Title          string
	Artist         string
	AlbumArtist    string
	Album          string
	Track          string
	Disc           string
	Year           string
	ReleaseDate    string
	RecordingID    string
	ReleaseID      string
	ReleaseGroupID string
	Picture        Picture
}

func RenderID3v23(tag Tag) ([]byte, error) {
	var frames bytes.Buffer
	for _, item := range []struct{ id, value string }{
		{"TIT2", tag.Title}, {"TPE1", tag.Artist}, {"TPE2", tag.AlbumArtist},
		{"TALB", tag.Album}, {"TRCK", tag.Track}, {"TPOS", tag.Disc}, {"TYER", tag.Year},
	} {
		if item.value != "" {
			writeFrame(&frames, item.id, textBody(item.value))
		}
	}
	for _, item := range []struct{ name, value string }{
		{"RELEASEDATE", tag.ReleaseDate},
		{"MusicBrainz Album Id", tag.ReleaseID},
		{"MusicBrainz Release Group Id", tag.ReleaseGroupID},
	} {
		if item.value != "" {
			writeFrame(&frames, "TXXX", userTextBody(item.name, item.value))
		}
	}
	if tag.RecordingID != "" {
		writeFrame(&frames, "UFID", append([]byte("http://musicbrainz.org\x00"), []byte(tag.RecordingID)...))
	}
	if len(tag.Picture.Data) > 0 {
		if tag.Picture.MIME != "image/jpeg" && tag.Picture.MIME != "image/png" {
			return nil, errors.New("id3: artwork must be JPEG or PNG")
		}
		body := []byte{0}
		body = append(body, tag.Picture.MIME...)
		body = append(body, 0, 3, 0) // MIME terminator, front cover, empty description.
		body = append(body, tag.Picture.Data...)
		writeFrame(&frames, "APIC", body)
	}

	payload := frames.Bytes()
	if len(payload) > 0x0fffffff {
		return nil, errors.New("id3: tag too large")
	}
	out := make([]byte, 10, 10+len(payload))
	copy(out, "ID3")
	out[3] = 3
	copy(out[6:], synchsafe(len(payload)))
	out = append(out, payload...)
	return out, nil
}

func writeFrame(dst *bytes.Buffer, id string, body []byte) {
	dst.WriteString(id)
	_ = binary.Write(dst, binary.BigEndian, uint32(len(body)))
	dst.Write([]byte{0, 0})
	dst.Write(body)
}

func textBody(value string) []byte {
	return append([]byte{1}, utf16LE(value, true)...)
}

func userTextBody(name, value string) []byte {
	body := []byte{1}
	body = append(body, utf16LE(name, true)...)
	body = append(body, 0, 0)
	body = append(body, utf16LE(value, true)...)
	return body
}

func utf16LE(value string, bom bool) []byte {
	units := utf16.Encode([]rune(value))
	out := make([]byte, 0, len(units)*2+2)
	if bom {
		out = append(out, 0xff, 0xfe)
	}
	for _, unit := range units {
		out = binary.LittleEndian.AppendUint16(out, unit)
	}
	return out
}

func synchsafe(size int) []byte {
	return []byte{byte(size >> 21), byte(size >> 14 & 0x7f), byte(size >> 7 & 0x7f), byte(size & 0x7f)}
}
