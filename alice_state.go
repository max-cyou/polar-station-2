package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type aliceStatus struct {
	State        string      `json:"state"`
	Available    bool        `json:"available"`
	Sequence     uint64      `json:"sequence"`
	MusicPlaying bool        `json:"musicPlaying"`
	Volume       float64     `json:"volume"`
	VolumeKnown  bool        `json:"volumeKnown"`
	VolumePulse  volumePulse `json:"volumePulse"`
}

type aliceMonitor struct {
	mu             sync.Mutex
	status         aliceStatus
	subscribers    map[chan aliceStatus]struct{}
	path           string
	previous       os.FileInfo
	offset         int64
	remainder      string
	musicPath      string
	musicPrevious  os.FileInfo
	musicOffset    int64
	musicRemainder string
	musicPlayers   map[string]bool
	lighting       lightingConfig
	volumeLive     bool
}

func newAliceMonitor(path string) *aliceMonitor {
	lighting, err := loadLighting("")
	if err != nil {
		panic(err)
	}
	return &aliceMonitor{path: path, lighting: lighting, musicPath: filepath.Join(filepath.Dir(path), "mediad.log"), musicPlayers: make(map[string]bool), status: aliceStatus{State: "unknown"}, subscribers: make(map[chan aliceStatus]struct{})}
}

func parseAliceState(line string) string {
	const marker = "onAliceStateChanged: "
	_, after, found := strings.Cut(line, marker)
	if !found {
		return ""
	}
	state := strings.FieldsFunc(after, func(r rune) bool { return r == ',' || r == ' ' || r == '\r' || r == '\n' })
	if len(state) == 0 {
		return ""
	}
	switch state[0] {
	case "IDLE":
		return "idle"
	case "LISTENING":
		return "listening"
	case "BUSY":
		return "busy"
	case "SPEAKING":
		return "speaking"
	default:
		return ""
	}
}

func (m *aliceMonitor) publish(state string, available bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State == state && m.status.Available == available {
		return
	}
	m.status.State = state
	m.status.Available = available
	m.status.Sequence++
	m.broadcastLocked()
}

func (m *aliceMonitor) broadcastLocked() {
	for ch := range m.subscribers {
		select {
		case ch <- m.status:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- m.status:
			default:
			}
		}
	}
}

func (m *aliceMonitor) snapshot() aliceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := m.status
	status.VolumePulse = status.VolumePulse.forDelivery()
	return status
}

// Read only appended log bytes. Handle both rename rotation and truncation.
func (m *aliceMonitor) poll() {
	file, err := os.Open(m.path)
	if err != nil {
		m.publish("unknown", false)
		m.previous = nil
		m.offset = 0
		m.remainder = ""
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		m.publish("unknown", false)
		return
	}
	if m.previous == nil || !os.SameFile(m.previous, info) || info.Size() < m.offset {
		m.offset = 0
		m.remainder = ""
	}
	m.previous = info
	const maxRead = 128 * 1024
	skipPartial := false
	if info.Size()-m.offset > maxRead {
		m.offset = info.Size() - maxRead
		m.remainder = ""
		skipPartial = true
	}
	data, err := io.ReadAll(io.NewSectionReader(file, m.offset, info.Size()-m.offset))
	if err != nil {
		m.publish("unknown", false)
		return
	}
	m.offset += int64(len(data))
	text := m.remainder + string(data)
	if skipPartial {
		_, text, _ = strings.Cut(text, "\n")
	}
	lines := strings.Split(text, "\n")
	m.remainder = lines[len(lines)-1]
	for _, line := range lines[:len(lines)-1] {
		m.volumeLine(line)
		if state := parseAliceState(line); state != "" {
			m.publish(state, true)
		}
	}
	m.volumeLive = true
}

func (m *aliceMonitor) run(ctx context.Context) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.poll()
			m.pollMusic()
		}
	}
}

func (m *aliceMonitor) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unavailable", 500)
		return
	}
	ch := make(chan aliceStatus, 16)
	m.mu.Lock()
	ch <- m.status
	m.subscribers[ch] = struct{}{}
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.subscribers, ch); m.mu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case state := <-ch:
			state.VolumePulse = state.VolumePulse.forDelivery()
			data, _ := json.Marshal(state)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
