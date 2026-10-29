package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const brightnessPath = "/sys/class/backlight/fb0/brightness"

func backlightMaximum() (int, error) {
	data, err := os.ReadFile("/sys/class/backlight/fb0/max_brightness")
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid maximum brightness")
	}
	return value, nil
}
func readBrightness() (int, error) {
	maximum, err := backlightMaximum()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(brightnessPath)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return (value*100 + maximum/2) / maximum, nil
}
func writeBrightness(percent int) error {
	maximum, err := backlightMaximum()
	if err != nil {
		return err
	}
	if percent < 0 || percent > 100 {
		return fmt.Errorf("invalid brightness")
	}
	return os.WriteFile(brightnessPath, []byte(strconv.Itoa((percent*maximum+50)/100)), 0600)
}
func (m *aliceMonitor) brightness(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != r.Host {
			authWrite(w, 403, map[string]string{"error": "Запрос с другого сайта запрещён"})
			return
		}
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		authWrite(w, 415, map[string]string{"error": "Нужен JSON-запрос"})
		return
	}
	var input struct {
		Brightness *int `json:"brightness"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil || input.Brightness == nil || *input.Brightness < 0 || *input.Brightness > 100 {
		authWrite(w, 400, map[string]string{"error": "Яркость должна быть от 0 до 100"})
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	before, err := readBrightness()
	if err != nil {
		authWrite(w, 503, map[string]string{"error": "Яркость панели недоступна"})
		return
	}
	if err = writeBrightness(*input.Brightness); err != nil {
		authWrite(w, 500, map[string]string{"error": "Не удалось изменить яркость панели"})
		return
	}
	config := m.lighting
	config.Brightness = input.Brightness
	data, err := json.MarshalIndent(config, "", "  ")
	if err == nil {
		var file *os.File
		file, err = os.CreateTemp(filepath.Dir(m.lightingPath), ".polar-brightness-*")
		if err == nil {
			defer os.Remove(file.Name())
			if _, err = file.Write(data); err == nil {
				err = file.Sync()
			}
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(file.Name(), m.lightingPath)
			}
		}
	}
	if err != nil {
		_ = writeBrightness(before)
		authWrite(w, 500, map[string]string{"error": "Не удалось сохранить яркость"})
		return
	}
	m.lighting = config
	authWrite(w, 200, map[string]int{"brightness": *input.Brightness})
}
