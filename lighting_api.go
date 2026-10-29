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
		Alice struct {
			Color   string `json:"color"`
			Enabled *bool  `json:"enabled"`
		} `json:"alice"`
		Volume struct {
			Low    string `json:"low"`
			Middle string `json:"middle"`
			High   string `json:"high"`
		} `json:"volume"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || input.Alice.Enabled == nil {
		authWrite(w, 400, map[string]string{"error": "Некорректные настройки"})
		return
	}
	for _, color := range []string{input.Alice.Color, input.Volume.Low, input.Volume.Middle, input.Volume.High} {
		if _, err := colorRGB(color); err != nil {
			authWrite(w, 400, map[string]string{"error": "Некорректный цвет"})
			return
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lightingPath == "" {
		authWrite(w, 503, map[string]string{"error": "Сохранение настроек недоступно"})
		return
	}
	config := m.lighting
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
		if physicalChanged {
			if rollbackErr := replaceVolumePattern(previousPattern); rollbackErr != nil {
				log.Printf("Restore volume: %v", rollbackErr)
			}
		}
		authWrite(w, 500, map[string]string{"error": "Не удалось сохранить настройки"})
		return
	}
	m.lighting = config
	authWrite(w, 200, config)
}
