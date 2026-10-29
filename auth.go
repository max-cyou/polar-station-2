package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// QR protocol: https://github.com/AlexxIT/YandexStation/blob/master/custom_components/yandex_station/core/yandex_session.py
const passportClientID = "c0ebe342af7d48fbbbfcf2d2eedb8f9e"
const passportClientSecret = "ad0a908f0aa341a182a37ecd75bc319e"
const musicClientID = "23cabbbdc6cd418abb4b39c32c41195d"
const musicClientSecret = "53bc75238f0c4d08a118e51fe9203300"
const authCookie = "polar_qr"
const authUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

var csrfPattern = regexp.MustCompile(`__CSRF__\s*=\s*"([^"]+)"`)
var errInvalidToken = errors.New("invalid Yandex token")

type authInfo struct {
	LoggedIn bool   `json:"loggedIn"`
	Login    string `json:"login,omitempty"`
}
type authCredentials struct {
	MusicToken string `json:"musicToken"`
	Login      string `json:"login"`
}
type qrSession struct {
	mu       sync.Mutex
	client   *http.Client
	csrf     string
	submit   map[string]any
	expires  time.Time
	complete bool
}
type yandexAuth struct {
	mu          sync.Mutex
	path        string
	credentials authCredentials
	verified    time.Time
	sessions    map[string]*qrSession
}

func newYandexAuth(path string) (*yandexAuth, error) {
	a := &yandexAuth{path: path, sessions: make(map[string]*qrSession)}
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &a.credentials); err != nil {
			return nil, fmt.Errorf("invalid auth file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return a, nil
}

func authRequest(ctx context.Context, client *http.Client, method, endpoint string, body io.Reader, headers map[string]string, result any) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", authUA)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("Не удалось связаться с Яндексом")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return errInvalidToken
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Яндекс вернул HTTP %d", response.StatusCode)
	}
	if result == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 2<<20))
		return err
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(result); err != nil {
		return errors.New("Не удалось прочитать ответ Яндекса")
	}
	return nil
}

func (q *qrSession) post(ctx context.Context, endpoint string, jsonBody any, form url.Values, result any) error {
	headers := map[string]string{"X-CSRF-Token": q.csrf}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
		headers["Content-Type"] = "application/x-www-form-urlencoded"
	} else {
		encoded, err := json.Marshal(jsonBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
		headers["Content-Type"] = "application/json"
	}
	return authRequest(ctx, q.client, http.MethodPost, endpoint, body, headers, result)
}

func userLogin(ctx context.Context, token string) (string, error) {
	var info struct {
		Login string `json:"login"`
	}
	err := authRequest(ctx, &http.Client{Timeout: 12 * time.Second}, http.MethodGet, "https://login.yandex.ru/info?format=json", nil, map[string]string{"Authorization": "OAuth " + token}, &info)
	if err != nil {
		return "", err
	}
	if info.Login == "" {
		return "", errInvalidToken
	}
	return info.Login, nil
}

func (a *yandexAuth) info(ctx context.Context) (authInfo, error) {
	a.mu.Lock()
	credentials, verified := a.credentials, a.verified
	a.mu.Unlock()
	if credentials.MusicToken == "" {
		return authInfo{}, nil
	}
	if time.Since(verified) < time.Minute {
		return authInfo{LoggedIn: true, Login: credentials.Login}, nil
	}
	login, err := userLogin(ctx, credentials.MusicToken)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.credentials.MusicToken != credentials.MusicToken {
		return authInfo{LoggedIn: a.credentials.MusicToken != "", Login: a.credentials.Login}, nil
	}
	if errors.Is(err, errInvalidToken) {
		if removeErr := os.Remove(a.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return authInfo{}, errors.New("Не удалось удалить отозванный токен")
		}
		a.credentials = authCredentials{}
		a.verified = time.Time{}
		return authInfo{}, nil
	}
	if err != nil {
		return authInfo{}, err
	}
	a.credentials.Login = login
	a.verified = time.Now()
	return authInfo{LoggedIn: true, Login: login}, nil
}

func (a *yandexAuth) start(ctx context.Context, previous string) (string, string, error) {
	jar, _ := cookiejar.New(nil)
	q := &qrSession{client: &http.Client{Jar: jar, Timeout: 12 * time.Second}, expires: time.Now().Add(5 * time.Minute)}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://passport.yandex.ru/pwl-yandex", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", authUA)
	response, err := q.client.Do(req)
	if err != nil {
		return "", "", errors.New("Не удалось открыть вход в Яндекс")
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 3<<20))
	response.Body.Close()
	if readErr != nil || response.StatusCode != 200 {
		return "", "", errors.New("Не удалось открыть вход в Яндекс")
	}
	match := csrfPattern.FindSubmatch(body)
	if len(match) != 2 {
		return "", "", errors.New("Яндекс изменил страницу QR-входа")
	}
	q.csrf = string(match[1])
	if err = q.post(ctx, "https://passport.yandex.ru/pwl-yandex/api/passport/auth/password/submit", map[string]string{"retpath": "https://passport.yandex.ru/"}, nil, &q.submit); err != nil {
		return "", "", err
	}
	track, _ := q.submit["track_id"].(string)
	if track == "" {
		return "", "", errors.New("Яндекс не создал сессию входа")
	}
	var code struct {
		Link string `json:"link"`
	}
	if err = q.post(ctx, "https://passport.yandex.ru/pwl-yandex/api/passport/auth/magic/code", nil, url.Values{"location_id": {"0"}, "magic_track_id": {track}, "track_id": {""}}, &code); err != nil {
		return "", "", err
	}
	link, err := url.Parse(code.Link)
	if err != nil || link.Scheme != "https" || !(link.Hostname() == "yandex.ru" || strings.HasSuffix(link.Hostname(), ".yandex.ru")) {
		return "", "", errors.New("Яндекс не выдал QR-код")
	}
	random := make([]byte, 24)
	if _, err = rand.Read(random); err != nil {
		return "", "", err
	}
	id := hex.EncodeToString(random)
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, previous)
	for key, session := range a.sessions {
		if time.Now().After(session.expires) {
			delete(a.sessions, key)
		}
	}
	if len(a.sessions) >= 8 {
		return "", "", errors.New("Слишком много попыток входа, попробуйте позже")
	}
	a.sessions[id] = q
	return id, code.Link, nil
}

func (a *yandexAuth) save(token, login string) error {
	credentials := authCredentials{MusicToken: token, Login: login}
	data, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(a.path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(a.path), ".polar-auth-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err = os.Rename(file.Name(), a.path); err != nil {
		return err
	}
	a.credentials = credentials
	a.verified = time.Now()
	return nil
}

func (a *yandexAuth) poll(ctx context.Context, id string) (string, authInfo, error) {
	a.mu.Lock()
	q := a.sessions[id]
	a.mu.Unlock()
	if q == nil {
		return "expired", authInfo{}, nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.complete {
		info, err := a.info(ctx)
		return "done", info, err
	}
	if time.Now().After(q.expires) {
		return "expired", authInfo{}, nil
	}
	var status struct {
		State   string `json:"state"`
		TrackID string `json:"trackId"`
	}
	if err := q.post(ctx, "https://passport.yandex.ru/pwl-yandex/api/passport/auth/magic/code/status", q.submit, nil, &status); err != nil {
		return "", authInfo{}, err
	}
	if status.State != "otp_auth_finished" {
		if strings.Contains(status.State, "expired") {
			return "expired", authInfo{}, nil
		}
		return "waiting", authInfo{}, nil
	}
	if status.TrackID == "" {
		return "", authInfo{}, errors.New("Яндекс не подтвердил вход")
	}
	if err := q.post(ctx, "https://passport.yandex.ru/pwl-yandex/api/passport/sessions/get_session", nil, url.Values{"track_id": {status.TrackID}}, nil); err != nil {
		return "", authInfo{}, err
	}
	passport, _ := url.Parse("https://passport.yandex.ru/")
	var cookies []string
	for _, cookie := range q.client.Jar.Cookies(passport) {
		cookies = append(cookies, cookie.Name+"="+cookie.Value)
	}
	var sessionToken struct {
		AccessToken string `json:"access_token"`
	}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Ya-Client-Host": "passport.yandex.ru", "Ya-Client-Cookie": strings.Join(cookies, "; ")}
	form := url.Values{"client_id": {passportClientID}, "client_secret": {passportClientSecret}}
	if err := authRequest(ctx, q.client, http.MethodPost, "https://mobileproxy.passport.yandex.net/1/bundle/oauth/token_by_sessionid", strings.NewReader(form.Encode()), headers, &sessionToken); err != nil {
		return "", authInfo{}, err
	}
	if sessionToken.AccessToken == "" {
		return "", authInfo{}, errors.New("Яндекс не выдал токен сессии")
	}
	var musicToken struct {
		AccessToken string `json:"access_token"`
	}
	form = url.Values{"client_id": {musicClientID}, "client_secret": {musicClientSecret}, "grant_type": {"x-token"}, "access_token": {sessionToken.AccessToken}}
	if err := authRequest(ctx, q.client, http.MethodPost, "https://oauth.mobile.yandex.net/1/token", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, &musicToken); err != nil {
		return "", authInfo{}, err
	}
	if musicToken.AccessToken == "" {
		return "", authInfo{}, errors.New("Яндекс не выдал токен Музыки")
	}
	login, err := userLogin(ctx, musicToken.AccessToken)
	if err != nil {
		return "", authInfo{}, err
	}
	if err = a.save(musicToken.AccessToken, login); err != nil {
		return "", authInfo{}, errors.New("Не удалось сохранить авторизацию на колонке")
	}
	q.complete = true
	return "done", authInfo{LoggedIn: true, Login: login}, nil
}

func authWrite(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (a *yandexAuth) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auth", func(w http.ResponseWriter, r *http.Request) {
		info, err := a.info(r.Context())
		if err != nil {
			authWrite(w, 503, map[string]string{"error": "Не удалось проверить авторизацию Яндекса"})
			return
		}
		authWrite(w, 200, info)
	})
	mux.HandleFunc("POST /api/auth/qr", func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host != r.Host {
				authWrite(w, 403, map[string]string{"error": "Запрос с другого сайта запрещён"})
				return
			}
		}
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if media != "application/json" {
			authWrite(w, 415, map[string]string{"error": "Нужен JSON-запрос"})
			return
		}
		previous := ""
		if cookie, err := r.Cookie(authCookie); err == nil {
			previous = cookie.Value
		}
		id, link, err := a.start(r.Context(), previous)
		if err != nil {
			authWrite(w, 502, map[string]string{"error": err.Error()})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: authCookie, Value: id, Path: "/api/auth", MaxAge: 300, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
		authWrite(w, 200, map[string]string{"link": link, "state": "waiting"})
	})
	mux.HandleFunc("GET /api/auth/qr", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(authCookie)
		if err != nil {
			authWrite(w, 200, map[string]string{"state": "expired"})
			return
		}
		state, info, err := a.poll(r.Context(), cookie.Value)
		if err != nil {
			authWrite(w, 502, map[string]string{"error": err.Error()})
			return
		}
		authWrite(w, 200, struct {
			State string `json:"state"`
			authInfo
		}{State: state, authInfo: info})
	})
}
