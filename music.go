package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type playerState struct {
	ID                  json.RawMessage `json:"id"`
	Title               string          `json:"title"`
	Subtitle            string          `json:"subtitle"`
	Progress            float64         `json:"progress"`
	Duration            float64         `json:"duration"`
	PlaylistType        string          `json:"playlistType"`
	PlaylistDescription string          `json:"playlistDescription"`
	PlaylistID          json.RawMessage `json:"playlistId"`
	PlaylistPUID        json.RawMessage `json:"playlistPuid"`
	EntityInfo          struct {
		Next struct {
			ID json.RawMessage `json:"id"`
		} `json:"next"`
		Shuffled *bool `json:"shuffled"`
	} `json:"entityInfo"`
	Extra struct {
		CoverURI string `json:"coverURI"`
	} `json:"extra"`
}
type glagolState struct {
	Playing bool         `json:"playing"`
	Player  *playerState `json:"playerState"`
	Volume  *float64     `json:"volume"`
}
type musicInfo struct {
	TrackKey  string  `json:"-"`
	TrackID   string  `json:"trackId"`
	Connected bool    `json:"connected"`
	Playing   bool    `json:"playing"`
	Title     string  `json:"title"`
	Artist    string  `json:"artist"`
	Cover     string  `json:"cover"`
	Duration  float64 `json:"duration"`
	Position  float64 `json:"position"`
	Error     string  `json:"error,omitempty"`
}
type musicClient struct {
	mu           sync.Mutex
	writeMu      sync.Mutex
	auth         *yandexAuth
	monitor      *aliceMonitor
	conn         *websocket.Conn
	conversation string
	state        glagolState
	updated      time.Time
	errorText    string
	waiters      map[string]chan bool
}

func (a *yandexAuth) musicToken() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.credentials.MusicToken
}
func newMusicClient(auth *yandexAuth, monitor *aliceMonitor) *musicClient {
	return &musicClient{auth: auth, monitor: monitor, waiters: make(map[string]chan bool)}
}
func messageID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[:4], data[4:6], data[6:8], data[8:10], data[10:])
}

func (m *musicClient) info() musicInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	info := musicInfo{Connected: m.conn != nil, Playing: m.state.Playing && m.conn != nil, Error: m.errorText}
	if player := m.state.Player; player != nil {
		info.TrackKey = string(player.ID) + "|" + player.Title + "|" + player.Subtitle
		var trackID string
		if json.Unmarshal(player.ID, &trackID) != nil {
			trackID = string(player.ID)
		}
		if match := musicTrackID.FindStringSubmatch(trackID); len(match) > 0 {
			info.TrackID = match[1]
		}
		info.Title, info.Artist = player.Title, player.Subtitle
		info.Duration = math.Max(0, player.Duration)
		info.Position = math.Max(0, player.Progress)
		if info.Playing && !m.updated.IsZero() {
			info.Position += time.Since(m.updated).Seconds()
		}
		if info.Duration > 0 {
			info.Position = math.Min(info.Position, info.Duration)
		}
		cover := strings.ReplaceAll(player.Extra.CoverURI, "%%", "400x400")
		if cover != "" && !strings.HasPrefix(cover, "https://") && !strings.HasPrefix(cover, "http://") {
			cover = "https://" + strings.TrimPrefix(cover, "//")
		}
		cover = strings.Replace(cover, "http://", "https://", 1)
		parsed, err := url.Parse(cover)
		if err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" {
			info.Cover = cover
		}
	}
	return info
}

func stationIdentity(ctx context.Context) (string, string, error) {
	var config struct {
		Common struct {
			DeviceType string `json:"deviceType"`
		} `json:"common"`
	}
	data, err := os.ReadFile("/system/vendor/quasar/quasar.cfg")
	if err != nil {
		return "", "", err
	}
	if err = json.Unmarshal(data, &config); err != nil {
		return "", "", err
	}
	if config.Common.DeviceType == "" {
		return "", "", errors.New("unknown station platform")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/system/vendor/quasar/maind", "--device-id")
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH=/system/vendor/quasar/libs:"+os.Getenv("LD_LIBRARY_PATH"))
	data, err = cmd.Output()
	if err != nil {
		return "", "", err
	}
	match := regexp.MustCompile(`Device ID:\s*([a-zA-Z0-9_-]+)`).FindSubmatch(data)
	if len(match) != 2 {
		return "", "", errors.New("station ID unavailable")
	}
	return string(match[1]), config.Common.DeviceType, nil
}

func (m *musicClient) write(conn *websocket.Conn, token string, payload map[string]any, id string) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteJSON(map[string]any{"conversationToken": token, "id": id, "payload": payload, "sentTime": time.Now().UnixMilli()})
}

func (m *musicClient) connect(ctx context.Context, deviceID, platform, token string) error {
	var response struct {
		Status string `json:"status"`
		Token  string `json:"token"`
	}
	query := url.Values{"device_id": {deviceID}, "platform": {platform}}
	if err := authRequest(ctx, &http.Client{Timeout: 12 * time.Second}, http.MethodGet, "https://quasar.yandex.net/glagol/token?"+query.Encode(), nil, map[string]string{"Authorization": "OAuth " + token}, &response); err != nil {
		return errors.New("Не удалось получить доступ к плееру Яндекса")
	}
	if response.Status != "ok" || response.Token == "" {
		return errors.New("Яндекс не предоставил доступ к этой Станции")
	}
	// The websocket belongs to this same device and has a self-signed certificate.
	dialer := websocket.Dialer{HandshakeTimeout: 8 * time.Second, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	conn, _, err := dialer.DialContext(ctx, "wss://127.0.0.1:1961", http.Header{"Origin": {"https://127.0.0.1"}})
	if err != nil {
		return errors.New("Не удалось соединиться с плеером Станции")
	}
	conn.SetReadLimit(2 << 20)
	m.mu.Lock()
	m.conn = conn
	m.conversation = response.Token
	m.errorText = ""
	m.mu.Unlock()
	stopped := make(chan struct{})
	defer func() {
		close(stopped)
		conn.Close()
		m.mu.Lock()
		if m.conn == conn {
			m.conn = nil
			m.conversation = ""
		}
		for id, ch := range m.waiters {
			select {
			case ch <- false:
			default:
			}
			delete(m.waiters, id)
		}
		m.mu.Unlock()
		m.monitor.setGlagolMusic(false, false)
	}()
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				conn.Close()
				return
			case <-stopped:
				return
			case <-ticker.C:
				if m.auth.musicToken() != token {
					conn.Close()
					return
				}
				if m.write(conn, response.Token, map[string]any{"command": "ping"}, messageID()) != nil {
					conn.Close()
					return
				}
			}
		}
	}()
	if err = m.write(conn, response.Token, map[string]any{"command": "softwareVersion"}, messageID()); err != nil {
		return errors.New("Плеер недоступен")
	}
	for {
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Second))
		var envelope struct {
			State     *glagolState    `json:"state"`
			RequestID string          `json:"requestId"`
			Status    json.RawMessage `json:"status"`
			Error     json.RawMessage `json:"error"`
		}
		if err = conn.ReadJSON(&envelope); err != nil {
			return errors.New("Соединение с плеером прервано")
		}
		m.mu.Lock()
		if envelope.State != nil {
			m.state = *envelope.State
			m.updated = time.Now()
		}
		if ch := m.waiters[envelope.RequestID]; ch != nil {
			var status string
			_ = json.Unmarshal(envelope.Status, &status)
			ok := (status == "" || strings.EqualFold(status, "success") || strings.EqualFold(status, "ok")) && (len(envelope.Error) == 0 || string(envelope.Error) == "null")
			select {
			case ch <- ok:
			default:
			}
		}
		playing := m.state.Playing && m.state.Player != nil
		m.mu.Unlock()
		if envelope.State != nil {
			m.monitor.setGlagolMusic(true, playing)
			if envelope.State.Volume != nil && *envelope.State.Volume >= 0 && *envelope.State.Volume <= 1 {
				m.monitor.syncVolume(*envelope.State.Volume * 10)
			}
		}
	}
}

func (m *musicClient) run(ctx context.Context) {
	var id, platform string
	for ctx.Err() == nil {
		token := m.auth.musicToken()
		if token != "" {
			var err error
			if id == "" {
				id, platform, err = stationIdentity(ctx)
			}
			if err == nil {
				err = m.connect(ctx, id, platform, token)
			}
			if err != nil {
				m.mu.Lock()
				m.errorText = err.Error()
				m.mu.Unlock()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (m *musicClient) command(ctx context.Context, payload map[string]any) error {
	before := m.info()
	id := messageID()
	ch := make(chan bool, 1)
	m.mu.Lock()
	conn, token := m.conn, m.conversation
	if conn == nil {
		m.mu.Unlock()
		return errors.New("Нет соединения с плеером")
	}
	m.waiters[id] = ch
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.waiters, id); m.mu.Unlock() }()
	if err := m.write(conn, token, payload, id); err != nil {
		return errors.New("Не удалось отправить команду плееру")
	}
	timer := time.NewTimer(7 * time.Second)
	defer timer.Stop()
	select {
	case ok := <-ch:
		if !ok {
			return errors.New("Плеер отклонил команду")
		}
		if command := payload["command"]; command == "play" || command == "stop" {
			return m.waitPlayback(ctx, conn, token, command == "play")
		}
		if command := payload["command"]; command == "next" || command == "prev" {
			return m.waitTrack(ctx, conn, token, before)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("Плеер не подтвердил команду")
	}
}

func (m *musicClient) waitTrack(ctx context.Context, conn *websocket.Conn, token string, before musicInfo) error {
	deadline := time.NewTimer(6 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	started := time.Now()
	for {
		info := m.info()
		if !info.Connected {
			return errors.New("Соединение с плеером прервано")
		}
		expectedPosition := before.Position
		if before.Playing {
			expectedPosition += time.Since(started).Seconds()
		}
		if info.TrackKey != before.TrackKey || (expectedPosition > 2 && info.Position < expectedPosition-1.5) {
			return nil
		}
		if err := m.write(conn, token, map[string]any{"command": "softwareVersion"}, messageID()); err != nil {
			return errors.New("Не удалось обновить состояние плеера")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Плеер не подтвердил переключение трека")
		case <-ticker.C:
		}
	}
}

func (m *musicClient) waitPlayback(ctx context.Context, conn *websocket.Conn, token string, playing bool) error {
	deadline := time.NewTimer(6 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		info := m.info()
		if !info.Connected {
			return errors.New("Соединение с плеером прервано")
		}
		if info.Playing == playing {
			return nil
		}
		if err := m.write(conn, token, map[string]any{"command": "softwareVersion"}, messageID()); err != nil {
			return errors.New("Не удалось обновить состояние плеера")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Плеер не подтвердил изменение воспроизведения")
		case <-ticker.C:
		}
	}
}

func (m *musicClient) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/music/queue", m.queueRoute)
	mux.HandleFunc("GET /api/music/like", m.likeRoute)
	mux.HandleFunc("POST /api/music/like", m.likeRoute)
	mux.HandleFunc("GET /api/music", func(w http.ResponseWriter, r *http.Request) {
		if m.auth.musicToken() == "" {
			authWrite(w, 401, map[string]string{"error": "Войдите в Яндекс"})
			return
		}
		authWrite(w, 200, m.info())
	})
	mux.HandleFunc("POST /api/music/command", func(w http.ResponseWriter, r *http.Request) {
		if m.auth.musicToken() == "" {
			authWrite(w, 401, map[string]string{"error": "Войдите в Яндекс"})
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
		var command struct {
			Command  string   `json:"command"`
			Position float64  `json:"position"`
			Volume   *float64 `json:"volume"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&command); err != nil {
			authWrite(w, 400, map[string]string{"error": "Некорректная команда"})
			return
		}
		payload := map[string]any{"command": command.Command}
		switch command.Command {
		case "play", "stop", "next", "prev":
		case "setVolume":
			if command.Volume == nil || *command.Volume < 0 || *command.Volume > 1 {
				authWrite(w, 400, map[string]string{"error": "Недопустимая громкость"})
				return
			}
			payload["volume"] = *command.Volume
		case "rewind":
			info := m.info()
			if info.Duration <= 0 || command.Position < 0 || command.Position > info.Duration {
				authWrite(w, 400, map[string]string{"error": "Недоступная позиция воспроизведения"})
				return
			}
			payload["position"] = command.Position
		default:
			authWrite(w, 400, map[string]string{"error": "Неизвестная команда плеера"})
			return
		}
		if err := m.command(r.Context(), payload); err != nil {
			authWrite(w, 502, map[string]string{"error": err.Error()})
			return
		}
		authWrite(w, 200, map[string]any{"ok": true, "music": m.info()})
	})
}
