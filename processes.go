package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type stationProcess struct {
	Started      string   `json:"started"`
	Nice         int      `json:"nice"`
	Controllable bool     `json:"controllable"`
	Clonable     bool     `json:"clonable"`
	Restartable  bool     `json:"restartable"`
	PID          int      `json:"pid"`
	Name         string   `json:"name"`
	CPU          *float64 `json:"cpu"`
	Memory       uint64   `json:"memory"`
	State        string   `json:"state"`
	ticks        uint64
	started      uint64
}

func readProcesses() (map[int]stationProcess, uint64, error) {
	cpu, ok := readCPU()
	if !ok {
		return nil, 0, fmt.Errorf("Не удалось прочитать нагрузку процессора")
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, 0, err
	}
	processes := make(map[int]stationProcess)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || !entry.IsDir() {
			continue
		}
		directory := filepath.Join("/proc", entry.Name())
		raw, err := os.ReadFile(filepath.Join(directory, "stat"))
		if err != nil {
			continue
		}
		line := string(raw)
		open, close := strings.IndexByte(line, '('), strings.LastIndexByte(line, ')')
		if open < 0 || close <= open {
			continue
		}
		fields := strings.Fields(line[close+1:])
		if len(fields) < 22 {
			continue
		}
		user, e1 := strconv.ParseUint(fields[11], 10, 64)
		system, e2 := strconv.ParseUint(fields[12], 10, 64)
		started, e3 := strconv.ParseUint(fields[19], 10, 64)
		pages, e4 := strconv.ParseInt(fields[21], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || pages < 0 {
			continue
		}
		name := line[open+1 : close]
		if name == "maind" {
			if args, err := os.ReadFile(filepath.Join(directory, "cmdline")); err == nil {
				parts := strings.Split(string(args), "\x00")
				if len(parts) > 1 {
					switch parts[1] {
					case "--platformd", "--mediad", "--telemetry", "--fluent-bitd", "--loggerd":
						name = "maind " + parts[1]
					}
				}
			}
		}
		nice, _ := strconv.Atoi(fields[16])
		executable, exeErr := os.Readlink(filepath.Join(directory, "exe"))
		controllable := pid > 1 && pid != os.Getpid() && exeErr == nil
		processes[pid] = stationProcess{Started: strconv.FormatUint(started, 10), Nice: nice, Controllable: controllable, Clonable: controllable && !strings.HasSuffix(executable, " (deleted)"), Restartable: controllable && !managedByQuasar(pid) && !strings.HasSuffix(executable, " (deleted)"), PID: pid, Name: name, Memory: uint64(pages) * uint64(os.Getpagesize()), State: fields[0], ticks: user + system, started: started}
	}
	return processes, cpu.total, nil
}
func processesHandler() http.HandlerFunc {
	var mu sync.Mutex
	previous, previousTotal, _ := readProcesses()
	var cached []stationProcess
	var sampled time.Time
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if cached != nil && time.Since(sampled) < 700*time.Millisecond {
			authWrite(w, 200, map[string]any{"processes": cached})
			return
		}
		current, total, err := readProcesses()
		if err != nil {
			authWrite(w, 503, map[string]string{"error": "Не удалось прочитать процессы"})
			return
		}
		list := make([]stationProcess, 0, len(current))
		for pid, process := range current {
			if before, ok := previous[pid]; ok && before.started == process.started && total > previousTotal && process.ticks >= before.ticks {
				value := min(100.0, 100*float64(process.ticks-before.ticks)/float64(total-previousTotal))
				process.CPU = &value
			}
			list = append(list, process)
		}
		sort.Slice(list, func(i, j int) bool {
			a, b := 0.0, 0.0
			if list[i].CPU != nil {
				a = *list[i].CPU
			}
			if list[j].CPU != nil {
				b = *list[j].CPU
			}
			if a != b {
				return a > b
			}
			if list[i].Memory != list[j].Memory {
				return list[i].Memory > list[j].Memory
			}
			return list[i].PID < list[j].PID
		})
		previous, previousTotal = current, total
		cached = list
		sampled = time.Now()
		authWrite(w, 200, map[string]any{"processes": list})
	}
}
