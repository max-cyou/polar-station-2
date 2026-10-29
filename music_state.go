package main

import (
	"io"
	"os"
	"strings"
)

func (m *aliceMonitor) setMusicPlaying(playing bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logMusicPlaying = playing
	if m.musicRemote {
		return
	}
	if m.status.MusicPlaying == playing {
		return
	}
	m.status.MusicPlaying = playing
	m.status.Sequence++
	m.broadcastLocked()
}

func (m *aliceMonitor) setGlagolMusic(connected, playing bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.musicRemote = connected
	if !connected {
		playing = m.logMusicPlaying
	}
	if m.status.MusicPlaying == playing {
		return
	}
	m.status.MusicPlaying = playing
	m.status.Sequence++
	m.broadcastLocked()
}

func (m *aliceMonitor) musicLine(line string) {
	// mediad labels TTS as [voice ...]. Only real audio streams count.
	_, rest, found := strings.Cut(line, "[audio stream=")
	if !found {
		return
	}
	player, _, found := strings.Cut(rest, "]")
	if !found {
		return
	}
	playing := strings.Contains(line, "Notification: Start") ||
		strings.Contains(line, "Notification: Resume") ||
		strings.Contains(line, "State changed from GST_STATE_PAUSED to GST_STATE_PLAYING")
	stopped := strings.Contains(line, "Notification: Pause") ||
		strings.Contains(line, "Notification: Stopped") ||
		strings.Contains(line, "Notification: End") ||
		strings.Contains(line, "Notification: Error") ||
		strings.Contains(line, "destroy instance") ||
		(strings.Contains(line, "switchUserState from ") &&
			(strings.Contains(line, " to PAUSED") || strings.Contains(line, " to STOPPED")))
	if playing {
		m.musicPlayers[player] = true
	}
	if stopped {
		delete(m.musicPlayers, player)
	}
	if playing || stopped {
		m.setMusicPlaying(len(m.musicPlayers) > 0)
	}
}

func (m *aliceMonitor) pollMusic() {
	file, err := os.Open(m.musicPath)
	if err != nil {
		m.musicPrevious = nil
		m.musicOffset = 0
		m.musicRemainder = ""
		clear(m.musicPlayers)
		m.setMusicPlaying(false)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return
	}
	if m.musicPrevious == nil || !os.SameFile(m.musicPrevious, info) || info.Size() < m.musicOffset {
		m.musicOffset = 0
		m.musicRemainder = ""
	}
	m.musicPrevious = info
	const maxRead = 256 * 1024
	skipPartial := false
	if info.Size()-m.musicOffset > maxRead {
		m.musicOffset = info.Size() - maxRead
		m.musicRemainder = ""
		skipPartial = true
	}
	data, err := io.ReadAll(io.NewSectionReader(file, m.musicOffset, info.Size()-m.musicOffset))
	if err != nil {
		return
	}
	m.musicOffset += int64(len(data))
	text := m.musicRemainder + string(data)
	if skipPartial {
		_, text, _ = strings.Cut(text, "\n")
	}
	lines := strings.Split(text, "\n")
	m.musicRemainder = lines[len(lines)-1]
	for _, line := range lines[:len(lines)-1] {
		m.musicLine(line)
	}
}
