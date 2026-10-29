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
	"strings"
	"time"
)

//go:embed dist
var frontend embed.FS

func newHandler(assets fs.FS, firmwarePath string, listen func(context.Context) error, monitors ...*aliceMonitor) http.Handler {
	mux := http.NewServeMux()
	monitor := newAliceMonitor("")
	if len(monitors) > 0 {
		monitor = monitors[0]
	}
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
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.Handle("/", http.FileServer(http.FS(assets)))
	return mux
}

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address")
	firmwarePath := flag.String("firmware-file", "/system/vendor/quasar/version_full", "Local firmware version file")
	aliceLog := flag.String("alice-log", "/data/quasar/daemons_logs/maind.log", "Alice state log")
	flag.Parse()
	monitor := newAliceMonitor(*aliceLog)
	monitor.poll()
	monitor.pollMusic()
	go monitor.run(context.Background())
	assets, err := fs.Sub(frontend, "dist")
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              *listen,
		Handler:           newHandler(assets, *firmwarePath, listenAlice, monitor),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Polar listening on %s", *listen)
	log.Fatal(server.ListenAndServe())
}
