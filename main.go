package main

import (
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed dist
var frontend embed.FS

func newHandler(assets fs.FS, firmwarePath string) http.Handler {
	mux := http.NewServeMux()
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
	flag.Parse()
	assets, err := fs.Sub(frontend, "dist")
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              *listen,
		Handler:           newHandler(assets, *firmwarePath),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Polar listening on %s", *listen)
	log.Fatal(server.ListenAndServe())
}
