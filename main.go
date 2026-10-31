package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed dist
var frontend embed.FS

func newHandler(assets fs.FS, firmwarePath string, listen func(context.Context) error, auth *yandexAuth, music *musicClient, monitors ...*aliceMonitor) http.Handler {
	mux := http.NewServeMux()
	filesRoutes(mux)
	mux.HandleFunc("GET /api/processes", processesHandler())
	mux.HandleFunc("POST /api/processes/action", processActionsHandler())
	mux.HandleFunc("GET /api/system", systemInfoHandler())
	mux.HandleFunc("POST /api/system/power", func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != r.Host {
				authWrite(w, http.StatusForbidden, map[string]string{"error": "Запрос с другого сайта запрещён"})
				return
			}
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			authWrite(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Нужен JSON-запрос"})
			return
		}
		var request struct {
			Action string `json:"action"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request) != nil || (request.Action != "reboot" && request.Action != "poweroff") {
			authWrite(w, http.StatusBadRequest, map[string]string{"error": "Неизвестное действие"})
			return
		}
		command := "/sbin/" + request.Action
		if _, err := os.Stat(command); err != nil {
			authWrite(w, http.StatusServiceUnavailable, map[string]string{"error": "Действие недоступно"})
			return
		}
		authWrite(w, http.StatusOK, map[string]bool{"ok": true})
		go func() {
			time.Sleep(time.Second)
			if err := exec.Command(command).Run(); err != nil {
				log.Printf("Power action: %v", err)
			}
		}()
	})
	if auth != nil {
		auth.routes(mux)
	}
	if music != nil {
		music.routes(mux)
	}
	monitor := newAliceMonitor("")
	if len(monitors) > 0 {
		monitor = monitors[0]
	}
	mux.HandleFunc("GET /api/appearance", monitor.appearance)
	mux.HandleFunc("POST /api/appearance", monitor.appearance)
	mux.HandleFunc("POST /api/appearance/brightness", monitor.brightness)
	mux.HandleFunc("GET /api/alice/events", monitor.events)
	mux.HandleFunc("GET /api/alice/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(monitor.snapshot())
	})
	aliceBusy := make(chan struct{}, 1)
	mux.HandleFunc("/api/alice/listen", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(status int, message string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			fail(http.StatusMethodNotAllowed, "Нужен POST-запрос")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != r.Host {
				fail(http.StatusForbidden, "Запрос с другого сайта запрещён")
				return
			}
		}
		mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaType != "application/json" {
			fail(http.StatusUnsupportedMediaType, "Нужен Content-Type: application/json")
			return
		}
		select {
		case aliceBusy <- struct{}{}:
			defer func() { <-aliceBusy }()
		default:
			fail(http.StatusConflict, "Алиса уже вызывается")
			return
		}
		if listen == nil {
			fail(http.StatusServiceUnavailable, "Вызов Алисы недоступен")
			return
		}
		if err := listen(r.Context()); err != nil {
			log.Printf("Alice listen: %v", err)
			fail(http.StatusServiceUnavailable, "Не удалось вызвать Алису")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/station", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		data, err := os.ReadFile(firmwarePath)
		if err != nil || strings.TrimSpace(string(data)) == "" {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Не удалось прочитать версию прошивки"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"firmware": strings.TrimSpace(string(data))})
	})
	mux.HandleFunc("GET /api/station/uptime", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile("/proc/uptime")
		fields := strings.Fields(string(data))
		if err != nil || len(fields) == 0 {
			authWrite(w, http.StatusServiceUnavailable, map[string]string{"error": "Время работы недоступно"})
			return
		}
		seconds, err := strconv.ParseFloat(fields[0], 64)
		if err != nil || seconds < 0 {
			authWrite(w, http.StatusServiceUnavailable, map[string]string{"error": "Время работы недоступно"})
			return
		}
		authWrite(w, http.StatusOK, map[string]any{"uptime": int64(seconds)})
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.Handle("/", http.FileServer(http.FS(assets)))
	return mux
}

func main() {
	executable, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	listen := flag.String("listen", ":8080", "HTTP listen address")
	firmwarePath := flag.String("firmware-file", "/system/vendor/quasar/version_full", "Local firmware version file")
	aliceLog := flag.String("alice-log", "/data/quasar/daemons_logs/maind.log", "Alice state log")
	lightingPath := flag.String("lighting-config", "", "Optional lighting JSON configuration")
	authPath := flag.String("auth-file", filepath.Join(filepath.Dir(executable), "auth.json"), "Persistent Yandex authorization file")
	flag.Parse()
	auth, err := newYandexAuth(*authPath)
	if err != nil {
		log.Fatal(err)
	}
	lighting, err := loadLighting(*lightingPath)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	monitor := newAliceMonitor(*aliceLog)
	monitor.lightingPath = filepath.Join(filepath.Dir(executable), "lighting.user.json")
	if _, err := os.Stat(monitor.lightingPath); err == nil {
		lighting, err = loadLighting(monitor.lightingPath)
		if err != nil {
			log.Fatal(err)
		}
	} else if !os.IsNotExist(err) {
		log.Fatal(err)
	}
	monitor.lighting = lighting
	if lighting.Brightness != nil {
		if err := writeBrightness(*lighting.Brightness); err != nil {
			log.Printf("Physical brightness: %v", err)
		}
	}
	monitor.poll()
	monitor.pollMusic()
	go monitor.run(ctx)

	music := newMusicClient(auth, monitor)
	go music.run(ctx)
	assets, err := fs.Sub(frontend, "dist")
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              *listen,
		Handler:           newHandler(assets, *firmwarePath, listenAlice, auth, music, monitor),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Polar listening on %s", *listen)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Print(err)
	}
}
