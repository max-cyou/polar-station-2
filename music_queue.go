package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

type queueTrack struct {
	ID      json.RawMessage `json:"id"`
	Title   string          `json:"title"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	CoverURI string `json:"coverUri"`
}
type queueItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Cover  string `json:"cover"`
}

func rawID(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return string(raw)
}
func queueTrackID(raw json.RawMessage) string {
	match := musicTrackID.FindStringSubmatch(rawID(raw))
	if len(match) == 0 {
		return ""
	}
	return match[1]
}
func queueItemFromTrack(t queueTrack) queueItem {
	var names []string
	for _, a := range t.Artists {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	cover := strings.ReplaceAll(t.CoverURI, "%%", "100x100")
	if cover != "" && !strings.HasPrefix(cover, "https://") {
		cover = "https://" + strings.TrimPrefix(cover, "//")
	}
	u, err := url.Parse(cover)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		cover = ""
	}
	return queueItem{ID: queueTrackID(t.ID), Title: t.Title, Artist: strings.Join(names, ", "), Cover: cover}
}
func (m *musicClient) queueRoute(w http.ResponseWriter, r *http.Request) {
	token := m.auth.musicToken()
	if token == "" {
		authWrite(w, 401, map[string]string{"error": "Войдите в Яндекс"})
		return
	}
	m.mu.Lock()
	if m.state.Player == nil || m.conn == nil {
		m.mu.Unlock()
		authWrite(w, 200, map[string]any{"tracks": []queueItem{}, "trackId": ""})
		return
	}
	player := *m.state.Player
	m.mu.Unlock()
	current, next := queueTrackID(player.ID), queueTrackID(player.EntityInfo.Next.ID)
	var tracks []queueTrack
	pid := rawID(player.PlaylistID)
	source := strings.TrimSpace(player.PlaylistDescription)
	if pid == "user:onyourwave" {
		source = "Моя волна"
	}
	// The catalogue order is meaningful only when the station confirms shuffle is off.
	if player.EntityInfo.Shuffled != nil && !*player.EntityInfo.Shuffled && pid != "" {
		switch player.PlaylistType {
		case "Album":
			if musicTrackID.MatchString(pid) {
				var result struct {
					Result struct {
						Title   string         `json:"title"`
						Volumes [][]queueTrack `json:"volumes"`
					} `json:"result"`
				}
				if musicAPI(r.Context(), token, http.MethodGet, "/albums/"+url.PathEscape(pid)+"/with-tracks", nil, &result) == nil {
					if result.Result.Title != "" {
						source = result.Result.Title
					}
					for _, volume := range result.Result.Volumes {
						tracks = append(tracks, volume...)
					}
				}
			}
		case "Playlist":
			owner, kind, ok := strings.Cut(pid, ":")
			if !ok {
				owner, kind = rawID(player.PlaylistPUID), pid
			}
			if musicTrackID.MatchString(owner) && musicTrackID.MatchString(kind) {
				var result struct {
					Result struct {
						Title  string `json:"title"`
						Tracks []struct {
							Track queueTrack `json:"track"`
						} `json:"tracks"`
					} `json:"result"`
				}
				if musicAPI(r.Context(), token, http.MethodGet, "/users/"+url.PathEscape(owner)+"/playlists/"+url.PathEscape(kind), nil, &result) == nil {
					if result.Result.Title != "" {
						source = result.Result.Title
					}
					for _, item := range result.Result.Tracks {
						tracks = append(tracks, item.Track)
					}
				}
			}
		}
	}
	if source == "" {
		var path string
		switch player.PlaylistType {
		case "Album":
			if musicTrackID.MatchString(pid) {
				path = "/albums/" + url.PathEscape(pid)
			}
		case "Playlist":
			owner, kind, ok := strings.Cut(pid, ":")
			if !ok {
				owner, kind = rawID(player.PlaylistPUID), pid
			}
			if musicTrackID.MatchString(owner) && musicTrackID.MatchString(kind) {
				path = "/users/" + url.PathEscape(owner) + "/playlists/" + url.PathEscape(kind)
			}
		}
		if path != "" {
			var result struct {
				Result struct {
					Title string `json:"title"`
				} `json:"result"`
			}
			if musicAPI(r.Context(), token, http.MethodGet, path, nil, &result) == nil {
				source = result.Result.Title
			}
		}
	}
	items := []queueItem{}
	for i, track := range tracks {
		if queueTrackID(track.ID) == current {
			for _, t := range tracks[i+1:] {
				item := queueItemFromTrack(t)
				if item.ID != "" && item.Title != "" {
					items = append(items, item)
				}
				if len(items) == 20 {
					break
				}
			}
			break
		}
	}
	// If the station reports a different next track, its actual queue takes precedence.
	if next != "" && (len(items) == 0 || items[0].ID != next) {
		var result struct {
			Result []queueTrack `json:"result"`
		}
		if musicAPI(r.Context(), token, http.MethodGet, "/tracks/"+url.PathEscape(next), nil, &result) == nil && len(result.Result) > 0 {
			items = []queueItem{queueItemFromTrack(result.Result[0])}
		} else {
			items = []queueItem{}
		}
	}
	authWrite(w, 200, map[string]any{"tracks": items, "trackId": current, "source": source})
}
