package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type stationFile struct {
	Name        string `json:"name"`
	Directory   bool   `json:"directory"`
	Link        bool   `json:"link"`
	Size        int64  `json:"size"`
	Mode        string `json:"mode"`
	Permissions string `json:"permissions"`
	Modified    int64  `json:"modified"`
}

func filePath(value string) (string, error) {
	if !filepath.IsAbs(value) || strings.ContainsRune(value, 0) {
		return "", errors.New("Укажите абсолютный путь")
	}
	return filepath.Clean(value), nil
}
func fileError(w http.ResponseWriter, err error) {
	status, message := 500, err.Error()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		status, message = 404, "Файл или каталог не найден"
	case errors.Is(err, fs.ErrExist):
		status, message = 409, "Файл с таким именем уже существует"
	case errors.Is(err, syscall.EROFS):
		status, message = 403, "Этот раздел доступен только для чтения"
	case errors.Is(err, fs.ErrPermission), errors.Is(err, syscall.EPERM):
		status, message = 403, "Недостаточно прав для выполнения действия"
	}
	authWrite(w, status, map[string]string{"error": message})
}

func fileOrigin(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host == r.Host
	}
	return true
}
func modeNumber(mode fs.FileMode) uint64 {
	n := uint64(mode.Perm())
	if mode&os.ModeSetuid != 0 {
		n |= 04000
	}
	if mode&os.ModeSetgid != 0 {
		n |= 02000
	}
	if mode&os.ModeSticky != 0 {
		n |= 01000
	}
	return n
}
func fileMode(n uint64) fs.FileMode {
	m := fs.FileMode(n & 0777)
	if n&04000 != 0 {
		m |= os.ModeSetuid
	}
	if n&02000 != 0 {
		m |= os.ModeSetgid
	}
	if n&01000 != 0 {
		m |= os.ModeSticky
	}
	return m
}

type fileContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r fileContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func copyEntry(ctx context.Context, source, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(link, target)
	}
	if info.IsDir() {
		if err = os.Mkdir(target, 0700); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err = copyEntry(ctx, filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		return os.Chmod(target, info.Mode())
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("Копирование специальных файлов не поддерживается: %s", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, fileContextReader{ctx, input})
	if err == nil {
		err = output.Chmod(info.Mode())
	}
	if err == nil {
		err = output.Sync()
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	return err
}
func copyFileTree(ctx context.Context, source, target string) error {
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		if err == nil {
			return fs.ErrExist
		}
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return err
	}
	canonicalTarget := filepath.Join(parent, filepath.Base(target))
	if info.IsDir() {
		canonicalSource, err := filepath.EvalSymlinks(source)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(canonicalSource, canonicalTarget)
		if err != nil {
			return err
		}
		if relative == "." || (!strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && relative != "..") {
			return errors.New("Нельзя скопировать каталог внутрь самого себя")
		}
	}
	staging, err := os.MkdirTemp(parent, ".polar-copy-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	staged := filepath.Join(staging, "content")
	if err = copyEntry(ctx, source, staged); err != nil {
		return err
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		if err == nil {
			return fs.ErrExist
		}
		return err
	}
	return os.Rename(staged, target)
}
func filesRoutes(mux *http.ServeMux) {
	var mutations sync.Mutex
	mux.HandleFunc("GET /api/files", func(w http.ResponseWriter, r *http.Request) {
		path, err := filePath(r.URL.Query().Get("path"))
		if err != nil {
			authWrite(w, 400, map[string]string{"error": err.Error()})
			return
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			fileError(w, err)
			return
		}
		files := make([]stationFile, 0, len(entries))
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			dir := info.IsDir()
			link := info.Mode()&os.ModeSymlink != 0
			if link {
				if target, err := os.Stat(filepath.Join(path, entry.Name())); err == nil {
					dir = target.IsDir()
				}
			}
			files = append(files, stationFile{Name: entry.Name(), Directory: dir, Link: link, Size: info.Size(), Mode: fmt.Sprintf("%04o", modeNumber(info.Mode())), Permissions: info.Mode().String(), Modified: info.ModTime().Unix()})
		}
		sort.Slice(files, func(i, j int) bool {
			if files[i].Directory != files[j].Directory {
				return files[i].Directory
			}
			return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
		})
		authWrite(w, 200, map[string]any{"path": path, "files": files})
	})
	mux.HandleFunc("POST /api/files/action", func(w http.ResponseWriter, r *http.Request) {
		if !fileOrigin(r) {
			authWrite(w, 403, map[string]string{"error": "Запрос с другого сайта запрещён"})
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			authWrite(w, 415, map[string]string{"error": "Нужен JSON-запрос"})
			return
		}
		var input struct {
			Action      string `json:"action"`
			Path        string `json:"path"`
			Destination string `json:"destination"`
			Mode        string `json:"mode"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input) != nil {
			authWrite(w, 400, map[string]string{"error": "Некорректный запрос"})
			return
		}
		path, err := filePath(input.Path)
		if err != nil || path == "/" {
			authWrite(w, 400, map[string]string{"error": "Недопустимый путь"})
			return
		}
		mutations.Lock()
		defer mutations.Unlock()
		switch input.Action {
		case "delete":
			err = os.RemoveAll(path)
		case "chmod":
			var n uint64
			n, err = strconv.ParseUint(input.Mode, 8, 12)
			if err == nil && (len(input.Mode) < 3 || len(input.Mode) > 4) {
				err = errors.New("Укажите права в формате 755 или 0755")
			}
			if err == nil {
				var info fs.FileInfo
				info, err = os.Lstat(path)
				if err == nil && info.Mode()&os.ModeSymlink != 0 {
					err = errors.New("Изменяйте права целевого файла, а не ссылки")
				}
				if err == nil {
					err = os.Chmod(path, fileMode(n))
				}
			}
		case "copy", "move":
			var target string
			target, err = filePath(input.Destination)
			if err != nil || target == "/" {
				authWrite(w, 400, map[string]string{"error": "Недопустимый путь назначения"})
				return
			}
			if input.Action == "copy" {
				err = copyFileTree(r.Context(), path, target)
			} else {
				if _, statErr := os.Lstat(target); !os.IsNotExist(statErr) {
					if statErr == nil {
						err = fs.ErrExist
					} else {
						err = statErr
					}
				} else {
					err = os.Rename(path, target)
					if errors.Is(err, syscall.EXDEV) {
						err = copyFileTree(r.Context(), path, target)
						if err == nil {
							err = os.RemoveAll(path)
						}
					}
				}
			}
		default:
			authWrite(w, 400, map[string]string{"error": "Неизвестное действие"})
			return
		}
		if err != nil {
			fileError(w, err)
			return
		}
		authWrite(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/files/upload", func(w http.ResponseWriter, r *http.Request) {
		if !fileOrigin(r) {
			authWrite(w, 403, map[string]string{"error": "Запрос с другого сайта запрещён"})
			return
		}
		path, err := filePath(r.URL.Query().Get("path"))
		if err != nil {
			authWrite(w, 400, map[string]string{"error": err.Error()})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
		reader, err := r.MultipartReader()
		if err != nil {
			authWrite(w, 400, map[string]string{"error": "Некорректная загрузка"})
			return
		}
		part, err := reader.NextPart()
		if err != nil {
			fileError(w, err)
			return
		}
		defer part.Close()
		name := part.FileName()
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
			authWrite(w, 400, map[string]string{"error": "Некорректное имя файла"})
			return
		}
		mutations.Lock()
		defer mutations.Unlock()
		target := filepath.Join(path, name)
		if _, err = os.Lstat(target); !os.IsNotExist(err) {
			if err == nil {
				err = fs.ErrExist
			}
			fileError(w, err)
			return
		}
		file, err := os.CreateTemp(path, ".polar-upload-*")
		if err != nil {
			fileError(w, err)
			return
		}
		defer os.Remove(file.Name())
		_, err = io.Copy(file, fileContextReader{r.Context(), part})
		if err == nil {
			err = file.Chmod(0644)
		}
		if err == nil {
			err = file.Sync()
		}
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(file.Name(), target)
		}
		if err != nil {
			fileError(w, err)
			return
		}
		authWrite(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/files/download", func(w http.ResponseWriter, r *http.Request) {
		path, err := filePath(r.URL.Query().Get("path"))
		if err != nil {
			authWrite(w, 400, map[string]string{"error": err.Error()})
			return
		}
		info, err := os.Stat(path)
		if err != nil {
			fileError(w, err)
			return
		}
		name := filepath.Base(path)
		if !info.IsDir() {
			if !info.Mode().IsRegular() {
				authWrite(w, 400, map[string]string{"error": "Можно скачивать только обычные файлы и каталоги"})
				return
			}
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.ServeFile(w, r, path)
			return
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".tar.gz"}))
		w.Header().Set("Content-Type", "application/gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		archive := tar.NewWriter(gz)
		defer archive.Close()
		err = filepath.WalkDir(path, func(file string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err = r.Context().Err(); err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
				return nil
			}
			link := ""
			if info.Mode()&os.ModeSymlink != 0 {
				link, err = os.Readlink(file)
				if err != nil {
					return err
				}
			}
			header, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(path, file)
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(filepath.Join(name, relative))
			if err = archive.WriteHeader(header); err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				input, err := os.Open(file)
				if err != nil {
					return err
				}
				defer input.Close()
				_, err = io.Copy(archive, fileContextReader{r.Context(), input})
				return err
			}
			return nil
		})
		if err != nil {
			panic(http.ErrAbortHandler)
		}
	})
}
