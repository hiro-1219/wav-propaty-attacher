package audio

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"wav-property-attacher/internal/metadata"
)

func ExportAIFF(source, destination string, tag metadata.Tag) (string, error) {
	wav, err := ReadWAV(source)
	if err != nil {
		return "", err
	}
	id3, err := metadata.RenderID3v23(tag)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".wpa-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(make([]byte, 12)); err != nil {
		return "", err
	}
	comm := make([]byte, 18)
	binary.BigEndian.PutUint16(comm[0:2], wav.Format.Channels)
	binary.BigEndian.PutUint32(comm[2:6], wav.Format.Frames)
	binary.BigEndian.PutUint16(comm[6:8], wav.Format.BitsPerSample)
	copy(comm[8:], extended80(float64(wav.Format.SampleRate)))
	if err = writeChunk(tmp, "COMM", comm); err != nil {
		return "", err
	}
	if err = writeChunk(tmp, "ID3 ", id3); err != nil {
		return "", err
	}
	if err = writeSSND(tmp, wav); err != nil {
		return "", err
	}

	end, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		return "", err
	}
	if end-8 > math.MaxUint32 {
		return "", errors.New("AIFF exceeds 4 GiB FORM limit")
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	header := make([]byte, 12)
	copy(header, "FORM")
	binary.BigEndian.PutUint32(header[4:8], uint32(end-8))
	copy(header[8:], "AIFF")
	if _, err = tmp.Write(header); err != nil {
		return "", err
	}
	if err = tmp.Sync(); err != nil {
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}

	verified, err := VerifyAIFF(tmpName, wav.Format.BitsPerSample)
	if err != nil {
		return "", err
	}
	if verified != wav.AudioHash {
		return "", errors.New("AIFF PCM verification hash mismatch")
	}
	if err = replaceFile(tmpName, destination); err != nil {
		return "", err
	}
	ok = true
	return verified, nil
}

func writeChunk(w io.Writer, id string, data []byte) error {
	if len(id) != 4 || uint64(len(data)) > math.MaxUint32 {
		return errors.New("invalid AIFF chunk")
	}
	if _, err := io.WriteString(w, id); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(data))); err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if len(data)&1 != 0 {
		_, err := w.Write([]byte{0})
		return err
	}
	return nil
}

func writeSSND(dst io.Writer, wav WAVInfo) error {
	if uint64(wav.DataSize)+8 > math.MaxUint32 {
		return errors.New("SSND chunk too large")
	}
	if _, err := io.WriteString(dst, "SSND"); err != nil {
		return err
	}
	if err := binary.Write(dst, binary.BigEndian, wav.DataSize+8); err != nil {
		return err
	}
	if err := binary.Write(dst, binary.BigEndian, uint32(0)); err != nil {
		return err
	}
	if err := binary.Write(dst, binary.BigEndian, uint32(0)); err != nil {
		return err
	}

	src, err := os.Open(wav.Path)
	if err != nil {
		return err
	}
	defer src.Close()
	if _, err := src.Seek(wav.DataOffset, io.SeekStart); err != nil {
		return err
	}
	width := int(wav.Format.BitsPerSample / 8)
	buf := make([]byte, 64*1024)
	remaining := int64(wav.DataSize)
	for remaining > 0 {
		next := int64(len(buf))
		if next > remaining {
			next = remaining
		}
		// Keep reads aligned so a sample never spans buffers.
		next -= next % int64(width)
		if next == 0 {
			next = remaining
		}
		n, err := io.ReadFull(src, buf[:next])
		if err != nil {
			return err
		}
		for i := 0; i < n; i += width {
			for left, right := i, i+width-1; left < right; left, right = left+1, right-1 {
				buf[left], buf[right] = buf[right], buf[left]
			}
		}
		if _, err := dst.Write(buf[:n]); err != nil {
			return err
		}
		remaining -= int64(n)
	}
	if wav.DataSize&1 != 0 {
		_, err := dst.Write([]byte{0})
		return err
	}
	return nil
}

func VerifyAIFF(path string, bits uint16) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, 12)
	if _, err := io.ReadFull(f, head); err != nil {
		return "", err
	}
	if string(head[:4]) != "FORM" || string(head[8:]) != "AIFF" {
		return "", errors.New("invalid AIFF header")
	}
	width := int(bits / 8)
	for {
		chunk := make([]byte, 8)
		if _, err := io.ReadFull(f, chunk); err != nil {
			return "", err
		}
		size := binary.BigEndian.Uint32(chunk[4:])
		if string(chunk[:4]) != "SSND" {
			if _, err := f.Seek(int64(size+(size&1)), io.SeekCurrent); err != nil {
				return "", err
			}
			continue
		}
		if size < 8 {
			return "", errors.New("invalid SSND chunk")
		}
		prefix := make([]byte, 8)
		if _, err := io.ReadFull(f, prefix); err != nil {
			return "", err
		}
		offset := binary.BigEndian.Uint32(prefix[:4])
		if offset > size-8 {
			return "", errors.New("invalid SSND offset")
		}
		if _, err := f.Seek(int64(offset), io.SeekCurrent); err != nil {
			return "", err
		}
		remaining := int64(size - 8 - offset)
		h := sha256.New()
		buf := make([]byte, 64*1024)
		for remaining > 0 {
			next := int64(len(buf))
			if next > remaining {
				next = remaining
			}
			next -= next % int64(width)
			if next == 0 {
				next = remaining
			}
			n, err := io.ReadFull(f, buf[:next])
			if err != nil {
				return "", err
			}
			for i := 0; i < n; i += width {
				for l, r := i, i+width-1; l < r; l, r = l+1, r-1 {
					buf[l], buf[r] = buf[r], buf[l]
				}
			}
			if _, err := h.Write(buf[:n]); err != nil {
				return "", err
			}
			remaining -= int64(n)
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
}

func extended80(value float64) []byte {
	out := make([]byte, 10)
	if value == 0 {
		return out
	}
	fraction, exponent := math.Frexp(value)
	fraction *= 2
	exponent--
	binary.BigEndian.PutUint16(out[:2], uint16(exponent+16383))
	mantissa := uint64(math.Ldexp(fraction, 63))
	binary.BigEndian.PutUint64(out[2:], mantissa)
	return out
}

func replaceFile(temp, destination string) error {
	if _, err := os.Stat(destination); err == nil {
		backup := destination + ".wpa-rollback"
		_ = os.Remove(backup)
		if err := os.Rename(destination, backup); err != nil {
			return err
		}
		if err := os.Rename(temp, destination); err != nil {
			_ = os.Rename(backup, destination)
			return err
		}
		_ = os.Remove(backup)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect destination: %w", err)
	}
	return os.Rename(temp, destination)
}
