package audio

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"wav-property-attacher/internal/domain"
)

var (
	ErrNotWAV      = errors.New("not a RIFF/WAVE file")
	ErrUnsupported = errors.New("WAV format is not supported by the rekordbox export profile")
)

type WAVInfo struct {
	Path       string
	Format     domain.AudioFormat
	DataOffset int64
	DataSize   uint32
	AudioHash  string
	Metadata   map[string]string
}

func ReadWAV(path string) (WAVInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return WAVInfo{}, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return WAVInfo{}, err
	}
	if stat.Size() < 12 {
		return WAVInfo{}, ErrNotWAV
	}
	header := make([]byte, 12)
	if _, err := io.ReadFull(f, header); err != nil {
		return WAVInfo{}, err
	}
	if string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		if string(header[:4]) == "RF64" || string(header[:4]) == "BW64" {
			return WAVInfo{}, fmt.Errorf("%w: RF64/BW64", ErrUnsupported)
		}
		return WAVInfo{}, ErrNotWAV
	}

	info := WAVInfo{Path: path, Metadata: map[string]string{}}
	var haveFormat, haveData bool
	for pos := int64(12); pos+8 <= stat.Size(); {
		if _, err := f.Seek(pos, io.SeekStart); err != nil {
			return WAVInfo{}, err
		}
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(f, chunkHeader); err != nil {
			return WAVInfo{}, fmt.Errorf("read chunk header: %w", err)
		}
		id := string(chunkHeader[:4])
		size := binary.LittleEndian.Uint32(chunkHeader[4:])
		dataPos := pos + 8
		end := dataPos + int64(size)
		if end < dataPos || end > stat.Size() {
			return WAVInfo{}, fmt.Errorf("invalid %q chunk size", id)
		}

		switch id {
		case "fmt ":
			buf := make([]byte, size)
			if _, err := io.ReadFull(f, buf); err != nil {
				return WAVInfo{}, err
			}
			format, err := parseFormat(buf)
			if err != nil {
				return WAVInfo{}, err
			}
			info.Format = format
			haveFormat = true
		case "data":
			if !haveData {
				info.DataOffset, info.DataSize = dataPos, size
				haveData = true
			}
		case "LIST":
			if size >= 4 {
				buf := make([]byte, size)
				if _, err := io.ReadFull(f, buf); err != nil {
					return WAVInfo{}, err
				}
				parseINFO(buf, info.Metadata)
			}
		}
		pos = end + int64(size&1)
	}
	if !haveFormat || !haveData {
		return WAVInfo{}, errors.New("WAV is missing fmt or data chunk")
	}
	if err := validateRekordboxFormat(info.Format, info.DataSize); err != nil {
		return WAVInfo{}, err
	}
	info.Format.Frames = info.DataSize / uint32(info.Format.BlockAlign)
	if _, err := f.Seek(info.DataOffset, io.SeekStart); err != nil {
		return WAVInfo{}, err
	}
	h := sha256.New()
	if _, err := io.CopyN(h, f, int64(info.DataSize)); err != nil {
		return WAVInfo{}, err
	}
	info.AudioHash = hex.EncodeToString(h.Sum(nil))
	return info, nil
}

func parseFormat(buf []byte) (domain.AudioFormat, error) {
	if len(buf) < 16 {
		return domain.AudioFormat{}, errors.New("WAV fmt chunk is too short")
	}
	code := binary.LittleEndian.Uint16(buf[0:2])
	if code == 0xfffe {
		if len(buf) < 40 || binary.LittleEndian.Uint16(buf[16:18]) < 22 {
			return domain.AudioFormat{}, fmt.Errorf("%w: invalid WAVE_FORMAT_EXTENSIBLE", ErrUnsupported)
		}
		// The first DWORD of KSDATAFORMAT_SUBTYPE_PCM is 1 in little endian.
		if binary.LittleEndian.Uint32(buf[24:28]) != 1 {
			return domain.AudioFormat{}, fmt.Errorf("%w: extensible subtype is not PCM", ErrUnsupported)
		}
	} else if code != 1 {
		return domain.AudioFormat{}, fmt.Errorf("%w: format code %d is not PCM", ErrUnsupported, code)
	}
	return domain.AudioFormat{
		Channels:      binary.LittleEndian.Uint16(buf[2:4]),
		SampleRate:    binary.LittleEndian.Uint32(buf[4:8]),
		BlockAlign:    binary.LittleEndian.Uint16(buf[12:14]),
		BitsPerSample: binary.LittleEndian.Uint16(buf[14:16]),
	}, nil
}

func validateRekordboxFormat(f domain.AudioFormat, dataSize uint32) error {
	if f.Channels < 1 || f.Channels > 2 {
		return fmt.Errorf("%w: channels must be mono or stereo", ErrUnsupported)
	}
	if f.BitsPerSample != 16 && f.BitsPerSample != 24 {
		return fmt.Errorf("%w: bit depth must be 16 or 24", ErrUnsupported)
	}
	switch f.SampleRate {
	case 44100, 48000, 88200, 96000, 192000:
	default:
		return fmt.Errorf("%w: sample rate %d Hz", ErrUnsupported, f.SampleRate)
	}
	expectedAlign := f.Channels * (f.BitsPerSample / 8)
	if f.BlockAlign != expectedAlign || f.BlockAlign == 0 || dataSize%uint32(f.BlockAlign) != 0 {
		return fmt.Errorf("%w: invalid PCM block alignment", ErrUnsupported)
	}
	return nil
}

func parseINFO(buf []byte, dst map[string]string) {
	if len(buf) < 4 || string(buf[:4]) != "INFO" {
		return
	}
	for pos := 4; pos+8 <= len(buf); {
		id := string(buf[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(buf[pos+4 : pos+8]))
		start, end := pos+8, pos+8+size
		if size < 0 || end > len(buf) {
			return
		}
		value := strings.TrimRight(string(buf[start:end]), "\x00")
		switch id {
		case "INAM":
			dst["title"] = value
		case "IART":
			dst["artist"] = value
		case "IPRD":
			dst["album"] = value
		case "ITRK":
			dst["track"] = value
		}
		pos = end + size%2
	}
}

func ScanAlbum(root string) ([]domain.LocalTrack, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	type fileHint struct {
		path, rel string
		disc      int
	}
	var files []fileHint
	addFiles := func(dir, relDir string, disc int) error {
		items, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, item := range items {
			if !item.Type().IsRegular() || !strings.EqualFold(filepath.Ext(item.Name()), ".wav") {
				continue
			}
			rel := item.Name()
			if relDir != "" {
				rel = filepath.Join(relDir, rel)
			}
			files = append(files, fileHint{filepath.Join(dir, item.Name()), rel, disc})
		}
		return nil
	}
	if err := addFiles(root, "", 1); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.EqualFold(entry.Name(), "rekordbox") {
			continue
		}
		disc, ok := discNumber(entry.Name())
		if !ok {
			continue
		}
		if err := addFiles(filepath.Join(root, entry.Name()), entry.Name(), disc); err != nil {
			return nil, err
		}
	}
	if len(files) == 0 {
		return nil, errors.New("対象のWAVファイルが見つかりません")
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].disc != files[j].disc {
			return files[i].disc < files[j].disc
		}
		ai, aj := numericPrefix(filepath.Base(files[i].rel)), numericPrefix(filepath.Base(files[j].rel))
		if ai != 0 && aj != 0 && ai != aj {
			return ai < aj
		}
		return strings.ToLower(files[i].rel) < strings.ToLower(files[j].rel)
	})
	tracks := make([]domain.LocalTrack, 0, len(files))
	for i, file := range files {
		info, err := ReadWAV(file.path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.rel, err)
		}
		trackHint := numericPrefix(filepath.Base(file.path))
		if trackHint == 0 {
			trackHint = i + 1
		}
		tracks = append(tracks, domain.LocalTrack{
			Path: file.path, Relative: file.rel, DiscHint: file.disc, TrackHint: trackHint,
			Duration: time.Duration(info.Format.Frames) * time.Second / time.Duration(info.Format.SampleRate),
			Format:   info.Format, AudioSHA256: info.AudioHash, Existing: info.Metadata,
			TitleHint:  trackTitleHint(filepath.Base(file.path), info.Metadata["title"]),
			ArtistHint: strings.TrimSpace(info.Metadata["artist"]),
		})
	}
	return tracks, nil
}

func trackTitleHint(name, tagged string) string {
	if value := strings.TrimSpace(tagged); value != "" {
		return value
	}
	value := strings.TrimSuffix(name, filepath.Ext(name))
	value = strings.TrimSpace(value)
	i := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	if i > 0 {
		value = strings.TrimLeft(value[i:], " _.-")
	}
	return strings.TrimSpace(value)
}

func discNumber(name string) (int, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, prefix := range []string{"disc", "disk", "cd"} {
		if strings.HasPrefix(n, prefix) {
			rest := strings.TrimLeft(strings.TrimPrefix(n, prefix), " _.-")
			v, err := strconv.Atoi(rest)
			return v, err == nil && v > 0
		}
	}
	return 0, false
}

func numericPrefix(name string) int {
	name = strings.TrimSpace(name)
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0
	}
	v, _ := strconv.Atoi(name[:i])
	return v
}
