package main

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type platformProcess struct {
	pid   int
	key   string
	retry int
}

// Linux exposes the clock tick frequency in auxv (AT_CLKTCK).
func clockTicks() (uint64, error) {
	data, err := os.ReadFile("/proc/self/auxv")
	if err != nil {
		return 0, err
	}
	width := strconv.IntSize / 8
	word := func(data []byte) uint64 {
		if width == 8 {
			return binary.NativeEndian.Uint64(data)
		}
		return uint64(binary.NativeEndian.Uint32(data))
	}
	for len(data) >= 2*width {
		if word(data) == 17 && word(data[width:]) > 0 {
			return word(data[width:]), nil
		}
		data = data[2*width:]
	}
	return 0, fmt.Errorf("clock frequency unavailable")
}

func currentPlatform() (platformProcess, error) {
	ticks, err := clockTicks()
	if err != nil {
		return platformProcess{}, err
	}
	uptime, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return platformProcess{}, err
	}
	fields := strings.Fields(string(uptime))
	if len(fields) == 0 {
		return platformProcess{}, fmt.Errorf("uptime unavailable")
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return platformProcess{}, err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return platformProcess{}, err
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return platformProcess{}, err
	}
	var found platformProcess
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		base := filepath.Join("/proc", entry.Name())
		exe, err := os.Readlink(filepath.Join(base, "exe"))
		if err != nil || exe != "/system/vendor/quasar/maind" {
			continue
		}
		command, err := os.ReadFile(filepath.Join(base, "cmdline"))
		if err != nil {
			continue
		}
		args := strings.Split(strings.TrimSuffix(string(command), "\x00"), "\x00")
		if len(args) != 2 || args[1] != "--platformd" || !managedByQuasar(pid) {
			continue
		}
		started, err := processIdentity(pid)
		if err != nil {
			continue
		}
		if found.pid != 0 {
			return platformProcess{}, fmt.Errorf("multiple platform processes")
		}
		// The station launcher limits maind restarts to one per 600 seconds.
		remaining := 600 - (seconds - float64(started)/float64(ticks))
		retry := 0
		if remaining > 0 {
			retry = int(remaining) + 1
		}
		found = platformProcess{pid: pid, key: fmt.Sprintf("%s:%d:%d", strings.TrimSpace(string(boot)), pid, started), retry: retry}
	}
	if found.pid == 0 {
		return found, fmt.Errorf("platform process unavailable")
	}
	return found, nil
}

func (m *aliceMonitor) appearanceApply(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && !fileOrigin(r) {
		authWrite(w, 403, map[string]string{"error": "Запрос с другого сайта запрещён"})
		return
	}
	platform, platformErr := currentPlatform()
	m.mu.Lock()
	marker, applying, applyError := m.lighting.PendingPlatform, m.applying, m.applyError
	m.mu.Unlock()
	pending := marker != "" && (platformErr != nil || marker == platform.key)
	if r.Method == http.MethodGet {
		authWrite(w, 200, map[string]any{"pending": pending, "applying": applying, "available": platformErr == nil, "retryAfter": platform.retry, "error": applyError})
		return
	}
	if !m.applyMu.TryLock() {
		authWrite(w, 409, map[string]string{"error": "Применение уже выполняется"})
		return
	}
	// Re-read after acquiring the save/apply lock: a save may have just finished.
	m.mu.Lock()
	marker = m.lighting.PendingPlatform
	m.mu.Unlock()
	if platformErr != nil {
		m.applyMu.Unlock()
		authWrite(w, 503, map[string]string{"error": "Служба Алисы недоступна"})
		return
	}
	if marker == "" || marker != platform.key {
		m.applyMu.Unlock()
		authWrite(w, 200, map[string]bool{"pending": false})
		return
	}
	if platform.retry > 0 {
		m.applyMu.Unlock()
		authWrite(w, 409, map[string]string{"error": fmt.Sprintf("Повторное применение будет доступно через %d с", platform.retry)})
		return
	}
	actual, err := currentPlatform()
	if err != nil || actual.key != platform.key {
		m.applyMu.Unlock()
		authWrite(w, 409, map[string]string{"error": "Служба уже перезапускается. Подождите"})
		return
	}
	if err := syscall.Kill(platform.pid, syscall.SIGTERM); err != nil {
		m.applyMu.Unlock()
		authWrite(w, 500, map[string]string{"error": "Не удалось перезапустить службу Алисы"})
		return
	}
	m.mu.Lock()
	m.applying = true
	m.applyError = ""
	m.mu.Unlock()
	go func() {
		defer m.applyMu.Unlock()
		deadline := time.Now().Add(60 * time.Second)
		var stableKey string
		var stableSince time.Time
		message := "Служба Алисы не восстановилась за минуту. Проверьте её состояние"
		for time.Now().Before(deadline) {
			time.Sleep(time.Second)
			next, err := currentPlatform()
			if err != nil || next.key == platform.key {
				stableKey = ""
				continue
			}
			if next.key != stableKey {
				stableKey = next.key
				stableSince = time.Now()
				continue
			}
			// Give the replacement process time to initialize and detect immediate exits.
			if time.Since(stableSince) >= 5*time.Second {
				message = ""
				break
			}
		}
		m.mu.Lock()
		m.applying = false
		m.applyError = message
		m.mu.Unlock()
	}()
	authWrite(w, 202, map[string]bool{"applying": true})
}
