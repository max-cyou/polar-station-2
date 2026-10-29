package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type filesystemInfo struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
}
type cpuCounters struct{ total, idle uint64 }

func readCPU() (cpuCounters, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuCounters{}, false
	}
	line, _, _ := strings.Cut(string(data), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuCounters{}, false
	}
	var c cpuCounters
	for i, field := range fields[1:] {
		if i >= 8 {
			break
		}
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuCounters{}, false
		}
		c.total += value
		if i == 3 || i == 4 {
			c.idle += value
		}
	}
	return c, true
}
func readMemory() (map[string]uint64, bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, false
	}
	values := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "MemTotal:" || fields[0] == "MemAvailable:" {
			n, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil {
				values[strings.TrimSuffix(fields[0], ":")] = n * 1024
			}
		}
	}
	total, okTotal := values["MemTotal"]
	available, okAvailable := values["MemAvailable"]
	if !okTotal || !okAvailable || available > total {
		return nil, false
	}
	return map[string]uint64{"total": total, "used": total - available}, true
}
func readFilesystem(path string) *filesystemInfo {
	var stat syscall.Statfs_t
	if syscall.Statfs(path, &stat) != nil {
		return nil
	}
	size := uint64(stat.Bsize)
	return &filesystemInfo{Total: stat.Blocks * size, Used: (stat.Blocks - stat.Bfree) * size, Available: stat.Bavail * size}
}
func systemInfoHandler() http.HandlerFunc {
	var mu sync.Mutex
	var previous cpuCounters
	previous, _ = readCPU()
	return func(w http.ResponseWriter, r *http.Request) {
		info := map[string]any{"memory": nil, "cpu": nil, "temperature": nil, "root": readFilesystem("/"), "data": readFilesystem("/data")}
		if memory, ok := readMemory(); ok {
			info["memory"] = memory
		}
		if raw, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
			if value, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64); err == nil {
				info["temperature"] = value / 1000
			}
		}
		if now, ok := readCPU(); ok {
			mu.Lock()
			before := previous
			previous = now
			mu.Unlock()
			if now.total > before.total && now.idle >= before.idle && now.idle-before.idle <= now.total-before.total {
				info["cpu"] = 100 * float64((now.total-before.total)-(now.idle-before.idle)) / float64(now.total-before.total)
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(info)
	}
}
