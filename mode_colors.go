package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var modePatternNames = map[string]string{"candle": "candle.led", "night": "night_light.led", "lava": "lavalamp.led"}

func validateModeColors(modes map[string][]string) error {
	if len(modes) != 3 {
		return fmt.Errorf("invalid mode palette")
	}
	for mode := range modePatternNames {
		colors := modes[mode]
		if (mode != "lava" && len(colors) != 1) || (mode == "lava" && (len(colors) < 2 || len(colors) > 6)) {
			return fmt.Errorf("invalid mode palette")
		}
		for _, color := range colors {
			if _, err := colorRGB(color); err != nil {
				return err
			}
		}
	}
	return nil
}
func renderModePattern(original []byte, colors []string) ([]byte, error) {
	palette := make([][3]byte, len(colors))
	for i, color := range colors {
		value, err := colorRGB(color)
		if err != nil {
			return nil, err
		}
		palette[i] = value
	}
	lines := strings.Split(string(original), "\n")
	frames := 0
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || (len(fields) == 1 && (fields[0] == "loop" || fields[0] == "no_size_optimization")) {
			continue
		}
		if len(fields) != 85 {
			return nil, fmt.Errorf("unexpected animation frame format")
		}
		for j := 0; j < 84; j++ {
			pixel, err := hex.DecodeString(fields[j])
			if err != nil || len(pixel) != 4 {
				return nil, fmt.Errorf("invalid animation pixel")
			}
			r, g, b := float64(pixel[0]), float64(pixel[1]), float64(pixel[2])
			maximum := math.Max(r, math.Max(g, b))
			minimum := math.Min(r, math.Min(g, b))
			brightness := math.Max(maximum, float64(pixel[3]))
			if brightness > 0 {
				hue := 0.0
				if delta := maximum - minimum; delta > 0 {
					switch maximum {
					case r:
						hue = math.Mod((g-b)/delta, 6)
					case g:
						hue = (b-r)/delta + 2
					default:
						hue = (r-g)/delta + 4
					}
					hue /= 6
					if hue < 0 {
						hue++
					}
				}
				position := hue * float64(len(palette))
				index := int(position) % len(palette)
				fraction := position - math.Floor(position)
				a, z := palette[index], palette[(index+1)%len(palette)]
				for channel := 0; channel < 3; channel++ {
					pixel[channel] = byte(math.Round((float64(a[channel])*(1-fraction) + float64(z[channel])*fraction) * brightness / 255))
				}
			}
			// The fourth byte drives a white LED; do not mix it into a chosen RGB color.
			pixel[3] = 0
			fields[j] = hex.EncodeToString(pixel)
		}
		lines[i] = strings.Join(fields, " ")
		frames++
	}
	if frames == 0 {
		return nil, fmt.Errorf("empty animation")
	}
	return []byte(strings.Join(lines, "\n")), nil
}
func saveModePatterns(next map[string][]string, directory string) (func(), error) {
	old := map[string][]byte{}
	rollback := func() {
		for target, data := range old {
			_ = replacePhysicalPattern(target, data)
		}
	}
	for _, mode := range []string{"candle", "night", "lava"} {
		// Always rebuild from the untouched original: old generated files may still contain white.
		name := modePatternNames[mode]
		target := filepath.Join(filepath.Dir(physicalVolumePath), name)
		current, err := os.ReadFile(target)
		if err != nil {
			return rollback, err
		}
		backupDir := filepath.Join(directory, "mode-pattern-originals")
		if err = os.MkdirAll(backupDir, 0700); err != nil {
			return rollback, err
		}
		originalPath := filepath.Join(backupDir, name)
		original, err := os.ReadFile(originalPath)
		if os.IsNotExist(err) {
			if err = os.WriteFile(originalPath, current, 0600); err != nil {
				return rollback, err
			}
			original = current
		} else if err != nil {
			return rollback, err
		}
		rendered, err := renderModePattern(original, next[mode])
		if err != nil {
			return rollback, err
		}
		if bytes.Equal(current, rendered) {
			continue
		}
		backup := filepath.Join(directory, "backup-"+mode+"-"+time.Now().Format("20060102-150405.000000000")+".led")
		if err = os.WriteFile(backup, current, 0600); err != nil {
			return rollback, err
		}
		old[target] = current
		if err = replacePhysicalPattern(target, rendered); err != nil {
			return rollback, err
		}
	}
	return rollback, nil
}
