package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (m *musicClient) lightingMode(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		authWrite(w, 200, map[string]string{"mode": m.monitor.snapshot().LightMode})
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
		Mode string `json:"mode"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil {
		authWrite(w, 400, map[string]string{"error": "Некорректный режим"})
		return
	}
	phrases := map[string]string{"candle": "включи режим свеча на этой колонке", "lava": "включи лава-лампу на этой колонке", "night": "включи режим ночник на этой колонке", "none": "выключи режим подсветки на этой колонке"}
	phrase, ok := phrases[input.Mode]
	if !ok {
		authWrite(w, 400, map[string]string{"error": "Неизвестный режим"})
		return
	}
	if m.auth.musicToken() == "" {
		authWrite(w, 401, map[string]string{"error": "Войдите в Яндекс"})
		return
	}
	if !m.modeBusy.TryLock() {
		authWrite(w, 409, map[string]string{"error": "Режим уже переключается"})
		return
	}
	defer m.modeBusy.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	// Native text requests can be ignored while Alice is finishing the previous reply.
	idleSince := time.Time{}
	for {
		state := m.monitor.snapshot().State
		if state == "idle" || state == "unknown" {
			if idleSince.IsZero() {
				idleSince = time.Now()
			}
			if time.Since(idleSince) >= 350*time.Millisecond {
				break
			}
		} else {
			idleSince = time.Time{}
		}
		select {
		case <-ctx.Done():
			authWrite(w, 504, map[string]string{"error": "Алиса занята, попробуйте ещё раз"})
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	if input.Mode == "none" {
		switch m.monitor.snapshot().LightMode {
		case "night":
			phrase = "выключи режим ночник"
		case "candle":
			phrase = "отключи режим свеча"
		case "lava":
			phrase = "выключи лава-лампу"
		}
	}
	if err := m.command(ctx, map[string]any{"command": "sendText", "text": phrase}); err != nil {
		authWrite(w, 502, map[string]string{"error": err.Error()})
		return
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for m.monitor.snapshot().LightMode != input.Mode {
		select {
		case <-ctx.Done():
			authWrite(w, 504, map[string]string{"error": "Колонка не подтвердила переключение режима"})
			return
		case <-ticker.C:
		}
	}
	authWrite(w, 200, map[string]any{"ok": true, "mode": input.Mode})
}
func (m *aliceMonitor) lightModeLine(line string) {
	if !strings.Contains(line, "Play animations:") {
		return
	}
	mode := ""
	switch {
	case strings.Contains(line, "candle.led"):
		mode = "candle"
	case strings.Contains(line, "lavalamp.led"):
		mode = "lava"
	case strings.Contains(line, "night_light.led"):
		mode = "night"
	case strings.Contains(line, "[ idle.led ] [ idle.led ]"):
		mode = "none"
	}
	if mode == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.LightMode != mode {
		m.status.LightMode = mode
		m.status.Sequence++
		m.broadcastLocked()
	}
}
