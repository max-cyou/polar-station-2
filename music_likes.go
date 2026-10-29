package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var musicTrackID = regexp.MustCompile(`^([0-9]+)(?::[0-9]+)?$`)

func (m *musicClient) trackID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Player == nil {
		return ""
	}
	raw := string(m.state.Player.ID)
	var id string
	if json.Unmarshal(m.state.Player.ID, &id) != nil {
		id = raw
	}
	match := musicTrackID.FindStringSubmatch(id)
	if len(match) == 0 {
		return ""
	}
	return match[1]
}

func musicAPI(ctx context.Context, token, method, path string, form url.Values, result any) error {
	headers := map[string]string{"Authorization": "OAuth " + token, "X-Yandex-Music-Client": "YandexMusicAndroid/24023621"}
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
		headers["Content-Type"] = "application/x-www-form-urlencoded"
	} else {
		body = strings.NewReader("")
	}
	return authRequest(ctx, &http.Client{Timeout: 12 * time.Second}, method, "https://api.music.yandex.net"+path, body, headers, result)
}

func musicUID(ctx context.Context, token string) (string, error) {
	var response struct {
		Result struct {
			Account struct {
				UID json.Number `json:"uid"`
			} `json:"account"`
		} `json:"result"`
	}
	if err := musicAPI(ctx, token, http.MethodGet, "/account/status", nil, &response); err != nil {
		return "", err
	}
	uid := string(response.Result.Account.UID)
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(uid) {
		return "", errors.New("Не удалось определить аккаунт")
	}
	return uid, nil
}

func (m *musicClient) likeRoute(w http.ResponseWriter, r *http.Request) {
	token := m.auth.musicToken()
	if token == "" {
		authWrite(w, 401, map[string]string{"error": "Войдите в Яндекс"})
		return
	}
	id := m.trackID()
	if id == "" || id != r.URL.Query().Get("track") {
		authWrite(w, 409, map[string]string{"error": "Трек недоступен или уже сменился"})
		return
	}
	if r.Method == http.MethodPost {
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != r.Host {
				authWrite(w, 403, map[string]string{"error": "Запрос с другого сайта запрещён"})
				return
			}
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			authWrite(w, 415, map[string]string{"error": "Нужен JSON-запрос"})
			return
		}
	}
	uid, err := musicUID(r.Context(), token)
	if err != nil {
		authWrite(w, 502, map[string]string{"error": "Не удалось получить аккаунт Яндекс Музыки"})
		return
	}
	path := "/users/" + uid + "/likes/tracks"
	if r.Method == http.MethodPost {
		var input struct {
			Liked *bool `json:"liked"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || input.Liked == nil {
			authWrite(w, 400, map[string]string{"error": "Некорректный запрос"})
			return
		}
		action := "/remove"
		if *input.Liked {
			action = "/add-multiple"
		}
		if err := musicAPI(r.Context(), token, http.MethodPost, path+action, url.Values{"track-ids": {id}}, nil); err != nil {
			authWrite(w, 502, map[string]string{"error": "Не удалось изменить отметку"})
			return
		}
		authWrite(w, 200, map[string]any{"liked": *input.Liked})
		return
	}
	var response struct {
		Result struct {
			Library struct {
				Tracks []struct {
					ID json.RawMessage `json:"id"`
				} `json:"tracks"`
			} `json:"library"`
		} `json:"result"`
	}
	if err := musicAPI(r.Context(), token, http.MethodGet, path, nil, &response); err != nil {
		authWrite(w, 502, map[string]string{"error": "Не удалось получить отметку"})
		return
	}
	liked := false
	for _, track := range response.Result.Library.Tracks {
		trackID := string(track.ID)
		var value string
		if json.Unmarshal(track.ID, &value) == nil {
			trackID = value
		}
		if match := musicTrackID.FindStringSubmatch(trackID); len(match) > 0 && match[1] == id {
			liked = true
			break
		}
	}
	authWrite(w, 200, map[string]any{"liked": liked})
}
