package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

func terminalHandler() http.HandlerFunc {
	sessions := make(chan struct{}, 2)
	upgrader := websocket.Upgrader{CheckOrigin: fileOrigin, HandshakeTimeout: 8 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		if !fileOrigin(r) {
			authWrite(w, 403, map[string]string{"error": "Запрос с другого сайта запрещён"})
			return
		}
		cols, errCols := strconv.Atoi(r.URL.Query().Get("cols"))
		rows, errRows := strconv.Atoi(r.URL.Query().Get("rows"))
		if errCols != nil || errRows != nil || cols < 2 || cols > 500 || rows < 2 || rows > 200 {
			authWrite(w, 400, map[string]string{"error": "Некорректный размер терминала"})
			return
		}
		select {
		case sessions <- struct{}{}:
			defer func() { <-sessions }()
		default:
			authWrite(w, 429, map[string]string{"error": "Уже открыты два терминала"})
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		cmd := exec.Command("/bin/sh", "-i")
		cmd.Dir = "/"
		if info, err := os.Stat("/data/opt"); err == nil && info.IsDir() {
			cmd.Dir = "/data/opt"
		}
		// Do not duplicate TERM entries inherited from the service environment.
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "TERM=") && !strings.HasPrefix(value, "COLORTERM=") {
				cmd.Env = append(cmd.Env, value)
			}
		}
		cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor")
		terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
		if err != nil {
			_ = conn.WriteJSON(map[string]string{"type": "error", "message": "Не удалось запустить оболочку"})
			return
		}
		defer terminal.Close()
		exited := make(chan struct{})
		var exitCode int
		go func() {
			err := cmd.Wait()
			if err != nil {
				exitCode = -1
				if failure, ok := err.(*exec.ExitError); ok {
					exitCode = failure.ExitCode()
				}
			}
			close(exited)
		}()
		// Closing the PTY hangs up its foreground process group. Also stop the shell
		// itself, and reap it before releasing the session slot.
		defer func() {
			_ = terminal.Close()
			select {
			case <-exited:
				return
			default:
			}
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
			select {
			case <-exited:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-exited
			}
		}()
		conn.SetReadLimit(32768)
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(90 * time.Second)) })
		readerDone := make(chan struct{})
		go func() {
			defer close(readerDone)
			defer terminal.Close()
			for {
				var message struct {
					Type string `json:"type"`
					Data string `json:"data"`
					Cols int    `json:"cols"`
					Rows int    `json:"rows"`
				}
				if err := conn.ReadJSON(&message); err != nil {
					return
				}
				switch message.Type {
				case "input":
					if _, err := terminal.Write([]byte(message.Data)); err != nil {
						return
					}
				case "resize":
					if message.Cols < 2 || message.Cols > 500 || message.Rows < 2 || message.Rows > 200 {
						return
					}
					if err := pty.Setsize(terminal, &pty.Winsize{Cols: uint16(message.Cols), Rows: uint16(message.Rows)}); err != nil {
						return
					}
				default:
					return
				}
			}
		}()
		heartbeatDone := make(chan struct{})
		defer close(heartbeatDone)
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-heartbeatDone:
					return
				case <-ticker.C:
					if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
						_ = conn.Close()
						_ = terminal.Close()
						return
					}
				}
			}
		}()
		// This handler is the sole data writer. Binary frames preserve UTF-8 bytes
		// split across PTY reads; xterm decodes them incrementally.
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(map[string]string{"type": "ready", "directory": cmd.Dir}); err != nil {
			return
		}
		output := make(chan []byte)
		outputDone := make(chan struct{})
		defer close(outputDone)
		go func() {
			defer close(output)
			buffer := make([]byte, 8192)
			for {
				n, err := terminal.Read(buffer)
				if n > 0 {
					data := append([]byte(nil), buffer[:n]...)
					select {
					case output <- data:
					case <-outputDone:
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()
	readOutput:
		for {
			select {
			case <-readerDone:
				return
			case data, ok := <-output:
				if !ok {
					break readOutput
				}
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
					return
				}
			}
		}
		select {
		case <-exited:
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_ = conn.WriteJSON(map[string]any{"type": "exit", "code": exitCode, "message": fmt.Sprintf("Оболочка завершена · код %d", exitCode)})
		case <-readerDone:
		case <-time.After(time.Second):
		}
	}
}
