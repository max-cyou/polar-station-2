package main

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed config/lighting.json
var lightingDefaults []byte

type volumeLighting struct {
	Low        string `json:"low"`
	Middle     string `json:"middle"`
	High       string `json:"high"`
	DurationMS int    `json:"durationMs"`
}

type lightingConfig struct {
	Volume volumeLighting `json:"volume"`
	Music  struct {
		AdaptiveBackground bool `json:"adaptiveBackground"`
	} `json:"music"`
}

type volumePulse struct {
	ID         uint64 `json:"id"`
	Color      string `json:"color"`
	DurationMS int    `json:"durationMs"`
	At         int64  `json:"at"`
	AgeMS      int64  `json:"ageMs"`
}

func (p volumePulse) forDelivery() volumePulse {
	p.AgeMS = max(0, time.Now().UnixMilli()-p.At)
	return p
}

func loadLighting(path string) (lightingConfig, error) {
	data := lightingDefaults
	var config lightingConfig
	if err := json.Unmarshal(lightingDefaults, &config); err != nil {
		return config, err
	}
	var err error
	if path != "" {
		data, err = os.ReadFile(path)
		if err != nil {
			return lightingConfig{}, err
		}
	}
	if err = json.Unmarshal(data, &config); err != nil {
		return config, err
	}
	for _, color := range []string{config.Volume.Low, config.Volume.Middle, config.Volume.High} {
		if _, err = colorRGB(color); err != nil {
			return config, err
		}
	}
	if config.Volume.DurationMS < 100 || config.Volume.DurationMS > 3000 {
		return config, fmt.Errorf("volume durationMs must be between 100 and 3000")
	}
	return config, nil
}

func colorRGB(color string) ([3]byte, error) {
	var rgb [3]byte
	if len(color) != 7 || color[0] != '#' {
		return rgb, fmt.Errorf("invalid lighting color %q", color)
	}
	data, err := hex.DecodeString(color[1:])
	if err == nil {
		copy(rgb[:], data)
	}
	return rgb, err
}

func (v volumeLighting) color(level float64) string {
	a, b := v.Low, v.Middle
	fraction := math.Max(0, math.Min(1, (level-1)/4))
	if level > 5 {
		a, b = v.Middle, v.High
		fraction = math.Max(0, math.Min(1, (level-5)/5))
	}
	start, _ := colorRGB(a)
	end, _ := colorRGB(b)
	var rgb [3]byte
	for i := range rgb {
		rgb[i] = byte(math.Round(float64(start[i])*(1-fraction) + float64(end[i])*fraction))
	}
	return fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2])
}

var volumeChange = regexp.MustCompile(`onVolumeChanged: platformVolume: ([0-9]+),.*stream: MEDIA`)

func (m *aliceMonitor) syncVolume(level float64) {
	level = math.Round(level*2) / 2
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.status.VolumeKnown || m.status.Volume != level {
		if m.status.VolumeKnown {
			m.status.VolumePulse = volumePulse{ID: m.status.VolumePulse.ID + 1, Color: m.lighting.Volume.color(level), DurationMS: m.lighting.Volume.DurationMS, At: time.Now().UnixMilli()}
		}
		m.status.Volume = level
		m.status.VolumeKnown = true
		m.status.Sequence++
		m.broadcastLocked()
	}
}

func (m *aliceMonitor) volumeLine(line string) {
	if !strings.Contains(line, "onVolumeChanged:") {
		return
	}
	match := volumeChange.FindStringSubmatch(line)
	if len(match) != 2 {
		return
	}
	platform, err := strconv.Atoi(match[1])
	if err != nil || platform > 20 {
		return
	}
	// Station 2 exposes 0..20 platform steps; Alice's user scale is 0..10.
	level := float64(platform) / 2
	m.mu.Lock()
	shouldPulse := m.status.VolumeKnown && level != m.status.Volume && m.volumeLive
	changed := !m.status.VolumeKnown || level != m.status.Volume
	m.status.Volume = level
	m.status.VolumeKnown = true
	var pulse volumePulse
	if shouldPulse {
		pulse = volumePulse{ID: m.status.VolumePulse.ID + 1, Color: m.lighting.Volume.color(level), DurationMS: m.lighting.Volume.DurationMS, At: time.Now().UnixMilli()}
		m.status.VolumePulse = pulse
	}
	if changed {
		m.status.Sequence++
		m.broadcastLocked()
	}
	m.mu.Unlock()
}
