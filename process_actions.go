package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func processIdentity(pid int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	line := string(data)
	end := strings.LastIndexByte(line, ')')
	if end < 0 {
		return 0, errors.New("Не удалось прочитать процесс")
	}
	fields := strings.Fields(line[end+1:])
	if len(fields) < 20 {
		return 0, errors.New("Не удалось прочитать процесс")
	}
	return strconv.ParseUint(fields[19], 10, 64)
}
func managedByQuasar(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return false
	}
	fields := strings.Fields(string(data)[end+1:])
	if len(fields) < 2 {
		return false
	}
	executable, err := os.Readlink("/proc/" + fields[1] + "/exe")
	return err == nil && strings.Contains(filepath.Base(executable), "quasar_launcher")
}
func processCommand(pid int) (*exec.Cmd, error) {
	base := fmt.Sprintf("/proc/%d", pid)
	executable, err := os.Readlink(filepath.Join(base, "exe"))
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(executable, " (deleted)") {
		return nil, errors.New("Исполняемый файл удалён, запустить копию нельзя")
	}
	raw, err := os.ReadFile(filepath.Join(base, "cmdline"))
	if err != nil {
		return nil, err
	}
	args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	if len(args) == 0 || args[0] == "" {
		return nil, errors.New("У процесса нет команды запуска")
	}
	cwd, err := os.Readlink(filepath.Join(base, "cwd"))
	if err != nil {
		return nil, err
	}
	environment, err := os.ReadFile(filepath.Join(base, "environ"))
	if err != nil {
		return nil, err
	}
	status, err := os.ReadFile(filepath.Join(base, "status"))
	if err != nil {
		return nil, err
	}
	credential := &syscall.Credential{}
	uidKnown, gidKnown := false, false
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "Uid:", "Gid:":
			if len(fields) < 3 {
				return nil, errors.New("Не удалось определить владельца процесса")
			}
			value, err := strconv.ParseUint(fields[2], 10, 32)
			if err != nil {
				return nil, err
			}
			if fields[0] == "Uid:" {
				credential.Uid = uint32(value)
				uidKnown = true
			} else {
				credential.Gid = uint32(value)
				gidKnown = true
			}
		case "Groups:":
			for _, field := range fields[1:] {
				value, err := strconv.ParseUint(field, 10, 32)
				if err != nil {
					return nil, err
				}
				credential.Groups = append(credential.Groups, uint32(value))
			}
		}
	}
	if !uidKnown || !gidKnown {
		return nil, errors.New("Не удалось определить владельца процесса")
	}
	cmd := exec.Command(executable, args[1:]...)
	cmd.Args = args
	cmd.Dir = cwd
	cmd.Env = []string{}
	if len(environment) > 0 {
		cmd.Env = strings.Split(strings.TrimSuffix(string(environment), "\x00"), "\x00")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Credential: credential}
	return cmd, nil
}
func processActionsHandler() http.HandlerFunc {
	var mu sync.Mutex
	return func(w http.ResponseWriter, r *http.Request) {
		if !fileOrigin(r) {
			authWrite(w, 403, map[string]string{"error": "Запрос с другого сайта запрещён"})
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			authWrite(w, 415, map[string]string{"error": "Нужен JSON-запрос"})
			return
		}
		var input struct {
			PID     int    `json:"pid"`
			Started string `json:"started"`
			Action  string `json:"action"`
			Nice    *int   `json:"nice"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
			authWrite(w, 400, map[string]string{"error": "Некорректный запрос"})
			return
		}
		if input.PID <= 1 || input.PID == os.Getpid() {
			authWrite(w, 403, map[string]string{"error": "Управление этим процессом недоступно"})
			return
		}
		switch input.Action {
		case "terminate", "kill", "pause", "resume", "clone", "restart", "priority":
		default:
			authWrite(w, 400, map[string]string{"error": "Неизвестное действие"})
			return
		}
		expected, err := strconv.ParseUint(input.Started, 10, 64)
		if err != nil || expected == 0 {
			authWrite(w, 400, map[string]string{"error": "Обновите список процессов"})
			return
		}
		if !mu.TryLock() {
			authWrite(w, 409, map[string]string{"error": "Предыдущее действие ещё выполняется"})
			return
		}
		defer mu.Unlock()
		identity, err := processIdentity(input.PID)
		if err != nil {
			fileError(w, err)
			return
		}
		if identity != expected {
			authWrite(w, 409, map[string]string{"error": "Процесс уже сменился. Обновите список"})
			return
		}
		if _, err = os.Readlink(fmt.Sprintf("/proc/%d/exe", input.PID)); err != nil {
			authWrite(w, 403, map[string]string{"error": "Управление этим процессом недоступно"})
			return
		}
		result := map[string]any{"ok": true}
		switch input.Action {
		case "clone", "restart":
			if input.Action == "restart" && managedByQuasar(input.PID) {
				authWrite(w, 409, map[string]string{"error": "Этот процесс перезапускает штатный супервизор. Используйте завершение"})
				return
			}
			var cmd *exec.Cmd
			cmd, err = processCommand(input.PID)
			if err == nil {
				var actual uint64
				actual, err = processIdentity(input.PID)
				if err == nil && actual != expected {
					err = errors.New("Процесс уже сменился")
				}
			}
			if err == nil && input.Action == "restart" {
				err = syscall.Kill(input.PID, syscall.SIGTERM)
				if err == nil {
					_ = syscall.Kill(input.PID, syscall.SIGCONT)
					deadline := time.Now().Add(5 * time.Second)
					for {
						actual, readErr := processIdentity(input.PID)
						if os.IsNotExist(readErr) || (readErr == nil && actual != expected) {
							break
						}
						// A zombie has exited and can be replaced even if its parent has not reaped it.
						stat, _ := os.ReadFile(fmt.Sprintf("/proc/%d/stat", input.PID))
						end := strings.LastIndexByte(string(stat), ')')
						if end >= 0 && strings.HasPrefix(strings.TrimSpace(string(stat)[end+1:]), "Z ") {
							break
						}
						if time.Now().After(deadline) {
							err = errors.New("Процесс не завершился. Можно отправить SIGKILL отдельно")
							break
						}
						time.Sleep(100 * time.Millisecond)
					}
				}
			}
			if err == nil {
				err = cmd.Start()
				if err == nil {
					result["pid"] = cmd.Process.Pid
					done := make(chan error, 1)
					go func() { done <- cmd.Wait() }()
					select {
					case exitErr := <-done:
						if exitErr != nil {
							err = fmt.Errorf("Новый экземпляр сразу завершился: %w", exitErr)
						} else {
							result["exited"] = true
						}
					case <-time.After(250 * time.Millisecond):
					}
				}
			}
		case "priority":
			if input.Nice == nil || *input.Nice < -20 || *input.Nice > 19 {
				authWrite(w, 400, map[string]string{"error": "Приоритет должен быть от −20 до 19"})
				return
			}
			err = syscall.Setpriority(syscall.PRIO_PROCESS, input.PID, *input.Nice)
		default:
			signal := map[string]syscall.Signal{"terminate": syscall.SIGTERM, "kill": syscall.SIGKILL, "pause": syscall.SIGSTOP, "resume": syscall.SIGCONT}[input.Action]
			// Recheck immediately before sending a signal to avoid acting on a reused PID.
			identity, err = processIdentity(input.PID)
			if err == nil && identity != expected {
				err = errors.New("Процесс уже сменился")
			}
			if err == nil {
				err = syscall.Kill(input.PID, signal)
			}
		}
		if err != nil {
			fileError(w, err)
			return
		}
		authWrite(w, 200, result)
	}
}
