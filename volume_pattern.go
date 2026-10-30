package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const physicalVolumePath = "/system/vendor/quasar/ledpatterns/panel/volume.led"

func renderVolumePattern(original []byte, palette volumeLighting) ([]byte, error) {
	low, err := colorRGB(palette.Low)
	if err != nil {
		return nil, err
	}
	middle, err := colorRGB(palette.Middle)
	if err != nil {
		return nil, err
	}
	high, err := colorRGB(palette.High)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(original), "\n")
	frames := 0
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || (len(fields) == 1 && fields[0] == "no_size_optimization") {
			continue
		}
		if len(fields) != 85 {
			return nil, fmt.Errorf("unexpected volume frame format")
		}
		for i := 0; i < 84; i++ {
			pixel, err := hex.DecodeString(fields[i])
			if err != nil || len(pixel) != 4 {
				return nil, fmt.Errorf("invalid volume pixel")
			}
			r, g, b := float64(pixel[0]), float64(pixel[1]), float64(pixel[2])
			brightness := math.Max(r, math.Max(g, b))
			if brightness > 0 {
				a, z, t := low, middle, r/math.Max(1, g)
				if r > g {
					a, z, t = middle, high, 1-g/math.Max(1, r)
				}
				for channel := 0; channel < 3; channel++ {
					pixel[channel] = byte(math.Round((float64(a[channel])*(1-t) + float64(z[channel])*t) * brightness / 255))
				}
			}
			fields[i] = hex.EncodeToString(pixel)
		}
		lines[index] = strings.Join(fields, " ")
		frames++
	}
	if frames != 300 {
		return nil, fmt.Errorf("unexpected volume frame count: %d", frames)
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func replaceVolumePattern(data []byte) error { return replacePhysicalPattern(physicalVolumePath, data) }

func replacePhysicalPattern(target string, data []byte) (err error) {
	var stat syscall.Statfs_t
	if err = syscall.Statfs("/", &stat); err != nil {
		return err
	}
	readonly := stat.Flags&1 != 0
	if readonly {
		if err = exec.Command("/bin/mount", "-o", "remount,rw", "/").Run(); err != nil {
			return err
		}
		defer func() {
			if restore := exec.Command("/bin/mount", "-o", "remount,ro", "/").Run(); restore != nil {
				err = fmt.Errorf("failed to restore read-only root: %w", restore)
			}
		}()
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".polar-pattern-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0644); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(file.Name(), target); err != nil {
		return err
	}
	syscall.Sync()
	return nil
}

func savePhysicalVolume(palette volumeLighting, directory string) (bool, []byte, error) {
	size, err := os.ReadFile("/sys/class/graphics/fb0/virtual_size")
	if err != nil || strings.TrimSpace(string(size)) != "89,1" {
		return false, nil, nil
	}
	original, err := os.ReadFile(filepath.Join(directory, "volume.original.led"))
	if err != nil {
		return false, nil, fmt.Errorf("original volume animation backup is unavailable: %w", err)
	}
	rendered, err := renderVolumePattern(original, palette)
	if err != nil {
		return false, nil, err
	}
	current, err := os.ReadFile(physicalVolumePath)
	if err != nil {
		return false, nil, err
	}
	if bytes.Equal(current, rendered) {
		return false, nil, nil
	}
	backup := filepath.Join(directory, "backup-volume-"+time.Now().Format("20060102-150405.000000000")+".led")
	if err = os.WriteFile(backup, current, 0600); err != nil {
		return false, nil, err
	}
	if err = replaceVolumePattern(rendered); err != nil {
		return false, current, err
	}
	return true, current, nil
}
