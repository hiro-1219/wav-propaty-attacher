package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type Fingerprint struct {
	Duration    float64 `json:"duration"`
	Fingerprint string  `json:"fingerprint"`
}

type FPCalc struct{ Path string }

func DefaultFPCalcPath() string {
	name := "fpcalc"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exe, err := os.Executable()
	if err != nil {
		return name
	}
	bundled := filepath.Join(filepath.Dir(exe), "tools", name)
	if _, err := os.Stat(bundled); err == nil {
		return bundled
	}
	if found, err := exec.LookPath(name); err == nil {
		return found
	}
	return bundled
}

func (f FPCalc) Calculate(ctx context.Context, path string) (Fingerprint, error) {
	tool := f.Path
	if tool == "" {
		tool = DefaultFPCalcPath()
	}
	cmd := exec.CommandContext(ctx, tool, "-json", path)
	output, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || os.IsNotExist(err) {
			return Fingerprint{}, errors.New("fpcalcが見つかりません。toolsフォルダを確認してください")
		}
		return Fingerprint{}, err
	}
	var result Fingerprint
	if err := json.Unmarshal(output, &result); err != nil {
		return Fingerprint{}, err
	}
	if result.Fingerprint == "" {
		return Fingerprint{}, errors.New("fpcalc returned an empty fingerprint")
	}
	return result, nil
}
