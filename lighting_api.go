package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func (m *aliceMonitor) appearance(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		m.mu.Lock()
		config := m.lighting
		m.mu.Unlock()
		if config.Brightness == nil {
			if value, err := readBrightness(); err == nil {
				config.Brightness = &value
			}
		}
		authWrite(w, http.StatusOK, config)
		return
	}
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
		Modes         map[string][]string `json:"modes"`
		CustomPresets *[]lightingPreset   `json:"customPresets"`
		ActivePreset  *string             `json:"activePreset"`
		Alerts        map[string]string   `json:"alerts"`
		Other         map[string]string   `json:"other"`
		Alice         struct {
			Color   string `json:"color"`
			Enabled *bool  `json:"enabled"`
		} `json:"alice"`
		Volume struct {
			Low    string `json:"low"`
			Middle string `json:"middle"`
			High   string `json:"high"`
		} `json:"volume"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&input) != nil || input.Alice.Enabled == nil {
		authWrite(w, 400, map[string]string{"error": "Некорректные настройки"})
		return
	}
	for _, color := range []string{input.Alice.Color, input.Volume.Low, input.Volume.Middle, input.Volume.High} {
		if _, err := colorRGB(color); err != nil {
			authWrite(w, 400, map[string]string{"error": "Некорректный цвет"})
			return
		}
	}
	for _, group := range []struct {
		values map[string]string
		keys   []string
	}{{input.Alerts, []string{"timer", "notification", "call"}}, {input.Other, []string{"bluetooth", "pause", "track", "like", "boot"}}} {
		if group.values == nil {
			continue
		}
		valid := len(group.values) == len(group.keys)
		for _, key := range group.keys {
			if _, err := colorRGB(group.values[key]); err != nil {
				valid = false
			}
		}
		if !valid {
			authWrite(w, 400, map[string]string{"error": "Некорректная палитра"})
			return
		}
	}
	if input.CustomPresets != nil {
		if len(*input.CustomPresets) > 64 {
			authWrite(w, 400, map[string]string{"error": "Слишком много пресетов"})
			return
		}
		ids := map[string]bool{}
		for _, preset := range *input.CustomPresets {
			if !strings.HasPrefix(preset.ID, "custom-") || len(preset.ID) > 100 || ids[preset.ID] || strings.TrimSpace(preset.Name) == "" || len(preset.Name) > 100 || len(preset.Colors) != 4 {
				authWrite(w, 400, map[string]string{"error": "Некорректный пресет"})
				return
			}
			if preset.Modes != nil {
				if err := validateModeColors(preset.Modes); err != nil {
					authWrite(w, 400, map[string]string{"error": "Некорректные цвета режимов пресета"})
					return
				}
			}
			ids[preset.ID] = true
			colors := append([]string(nil), preset.Colors...)
			for _, group := range []map[string]string{preset.Alerts, preset.Other} {
				for _, color := range group {
					colors = append(colors, color)
				}
			}
			for _, color := range colors {
				if _, err := colorRGB(color); err != nil {
					authWrite(w, 400, map[string]string{"error": "Некорректный цвет пресета"})
					return
				}
			}
		}
	}
	if input.ActivePreset != nil && len(*input.ActivePreset) > 100 {
		authWrite(w, 400, map[string]string{"error": "Некорректный пресет"})
		return
	}
	if input.Modes != nil {
		if err := validateModeColors(input.Modes); err != nil {
			authWrite(w, 400, map[string]string{"error": "Некорректные цвета режимов"})
			return
		}
	}
	if !m.applyMu.TryLock() {
		authWrite(w, 409, map[string]string{"error": "Дождитесь применения подсветки"})
		return
	}
	defer m.applyMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lightingPath == "" {
		authWrite(w, 503, map[string]string{"error": "Сохранение настроек недоступно"})
		return
	}
	config := m.lighting
	if input.Modes != nil {
		config.Modes = input.Modes
	}
	if input.CustomPresets != nil {
		config.CustomPresets = *input.CustomPresets
	}
	if input.ActivePreset != nil {
		config.ActivePreset = *input.ActivePreset
	}
	if input.Alerts != nil {
		config.Alerts = input.Alerts
	}
	if input.Other != nil {
		config.Other = input.Other
	}
	config.Alice.Color, config.Alice.Enabled = input.Alice.Color, *input.Alice.Enabled
	config.Volume.Low, config.Volume.Middle, config.Volume.High = input.Volume.Low, input.Volume.Middle, input.Volume.High
	physicalChanged, previousPattern, physicalErr := savePhysicalVolume(config.Volume, filepath.Dir(m.lightingPath))
	if physicalErr != nil {
		log.Printf("Save physical volume: %v", physicalErr)
		if previousPattern != nil {
			if rollbackErr := replaceVolumePattern(previousPattern); rollbackErr != nil {
				log.Printf("Restore volume: %v", rollbackErr)
			}
		}
		authWrite(w, 500, map[string]string{"error": "Не удалось записать физическую палитру. Настройки не сохранены"})
		return
	}
	rollbackModes, modesChanged, modeErr := saveModePatterns(config.Modes, filepath.Dir(m.lightingPath))
	if modeErr != nil {
		rollbackModes()
		if physicalChanged {
			_ = replaceVolumePattern(previousPattern)
		}
		log.Printf("Save mode colors: %v", modeErr)
		authWrite(w, 500, map[string]string{"error": "Не удалось записать цвета режимов"})
		return
	}
	if physicalChanged || modesChanged {
		config.PendingPlatform = "unknown"
		if platform, err := currentPlatform(); err == nil {
			config.PendingPlatform = platform.key
		}
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err == nil {
		var file *os.File
		file, err = os.CreateTemp(filepath.Dir(m.lightingPath), ".polar-lighting-*")
		if err == nil {
			name := file.Name()
			defer os.Remove(name)
			if _, err = file.Write(data); err == nil {
				err = file.Sync()
			}
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(name, m.lightingPath)
			}
		}
	}
	if err != nil {
		rollbackModes()
		if physicalChanged {
			if rollbackErr := replaceVolumePattern(previousPattern); rollbackErr != nil {
				log.Printf("Restore volume: %v", rollbackErr)
			}
		}
		authWrite(w, 500, map[string]string{"error": "Не удалось сохранить настройки"})
		return
	}
	m.lighting = config
	m.applyError = ""
	authWrite(w, 200, config)
}
