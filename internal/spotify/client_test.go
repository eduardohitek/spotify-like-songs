package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient()
	c.APIURL = srv.URL
	c.TokenURL = srv.URL + "/token"
	c.accessToken = "test-token"
	return c, srv
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encoding response: %v", err)
	}
}

func TestAuthenticate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "id" || pass != "secret" {
			t.Errorf("basic auth = %q, %q, %v", user, pass, ok)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q", got)
		}
		if got := r.PostForm.Get("refresh_token"); got != "a+b/c=" {
			t.Errorf("refresh_token = %q, want it URL-decoded intact", got)
		}
		writeJSON(t, w, http.StatusOK, map[string]string{"access_token": "new-access", "refresh_token": "rotated"})
	})
	mux.HandleFunc("GET /me/playlists", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer new-access" {
			t.Errorf("Authorization = %q", got)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"items": []any{}})
	})
	c, _ := newTestClient(t, mux)

	token, err := c.Authenticate(context.Background(), "id", "secret", "a+b/c=")
	if err != nil {
		t.Fatal(err)
	}
	if token.RefreshToken != "rotated" {
		t.Errorf("RefreshToken = %q", token.RefreshToken)
	}
	if _, err := c.FindPlaylist(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
}

func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"web api", `{"error":{"status":403,"message":"Forbidden"}}`, "Forbidden"},
		{"accounts", `{"error":"invalid_grant","error_description":"Refresh token revoked"}`, "invalid_grant: Refresh token revoked"},
		{"plain", "upstream down\n", "upstream down"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, tt.body)
			}))
			_, err := c.FindPlaylist(context.Background(), "x")
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.StatusCode != http.StatusBadRequest || apiErr.Message != tt.want {
				t.Errorf("got %d %q, want 400 %q", apiErr.StatusCode, apiErr.Message, tt.want)
			}
		})
	}
}

func TestLikedSongsSinceStopsAtOlderSong(t *testing.T) {
	since := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	song := func(id string, addedAt time.Time) SavedTrack {
		return SavedTrack{AddedAt: addedAt, Track: Track{ID: id}}
	}
	var srvURL string
	pages := map[string]any{
		"": map[string]any{
			"items": []SavedTrack{song("a", since.Add(48*time.Hour)), song("b", since)},
			"next":  "/me/tracks?page=2",
		},
		"2": map[string]any{
			"items": []SavedTrack{song("c", since.Add(-time.Second)), song("d", since.Add(-time.Hour))},
			"next":  "/me/tracks?page=3",
		},
	}
	c, srv := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, ok := pages[r.URL.Query().Get("page")]
		if !ok {
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body := page.(map[string]any)
		body["next"] = srvURL + body["next"].(string)
		writeJSON(t, w, http.StatusOK, body)
	}))
	srvURL = srv.URL

	songs, err := c.LikedSongsSince(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 2 || songs[0].Track.ID != "a" || songs[1].Track.ID != "b" {
		t.Errorf("songs = %+v, want a, b", songs)
	}
}

func TestFindPlaylistPaginates(t *testing.T) {
	var srvURL string
	c, srv := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"items": []any{nil, map[string]string{"id": "1", "name": "Sep'26"}},
				"next":  srvURL + "/me/playlists?page=2",
			})
		case "2":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"items": []any{map[string]string{"id": "2", "name": "Oct'26"}},
				"next":  nil,
			})
		}
	}))
	srvURL = srv.URL

	found, err := c.FindPlaylist(context.Background(), "Oct'26")
	if err != nil {
		t.Fatal(err)
	}
	if found == nil || found.ID != "2" {
		t.Errorf("found = %+v, want id 2", found)
	}

	missing, err := c.FindPlaylist(context.Background(), "Nov'26")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Errorf("missing = %+v, want nil", missing)
	}
}

func TestCreatePlaylistSendsBooleanPublic(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/me/playlists" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["public"] != false || body["name"] != "Oct'26" || body["description"] != "desc" {
			t.Errorf("body = %v", body)
		}
		writeJSON(t, w, http.StatusCreated, map[string]string{"id": "new", "name": "Oct'26"})
	}))

	playlist, err := c.CreatePlaylist(context.Background(), "Oct'26", "desc", false)
	if err != nil {
		t.Fatal(err)
	}
	if playlist.ID != "new" {
		t.Errorf("ID = %q", playlist.ID)
	}
}

func TestPlaylistTrackIDs(t *testing.T) {
	var srvURL string
	c, srv := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/playlists/pl/items" {
			t.Errorf("path = %s", r.URL.Path)
		}
		switch r.URL.Query().Get("page") {
		case "":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"items": []any{map[string]any{"item": map[string]string{"id": "a"}}, map[string]any{"item": nil}},
				"next":  srvURL + "/playlists/pl/items?page=2",
			})
		case "2":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"items": []any{map[string]any{"item": map[string]string{"id": "b"}}},
			})
		}
	}))
	srvURL = srv.URL

	ids, err := c.PlaylistTrackIDs(context.Background(), "pl")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || !ids["a"] || !ids["b"] {
		t.Errorf("ids = %v, want a and b", ids)
	}
}

func TestAddTracksBatches(t *testing.T) {
	var batches [][]string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/playlists/pl/items" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			URIs []string `json:"uris"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		batches = append(batches, body.URIs)
		writeJSON(t, w, http.StatusCreated, map[string]string{"snapshot_id": "s"})
	}))

	ids := make([]string, 150)
	for i := range ids {
		ids[i] = fmt.Sprintf("t%d", i)
	}
	if err := c.AddTracks(context.Background(), "pl", ids); err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || len(batches[0]) != 100 || len(batches[1]) != 50 {
		t.Fatalf("batch sizes = %d batches", len(batches))
	}
	if batches[0][0] != "spotify:track:t0" || batches[1][49] != "spotify:track:t149" {
		t.Errorf("uris = %q ... %q", batches[0][0], batches[1][49])
	}

	if err := c.AddTracks(context.Background(), "pl", nil); err != nil || len(batches) != 2 {
		t.Errorf("AddTracks(nil) made a request or failed: %v", err)
	}
}

func TestArtistNames(t *testing.T) {
	tests := []struct {
		track Track
		want  string
	}{
		{Track{}, "unknown artist"},
		{Track{Artists: []Artist{{Name: "A"}}}, "A"},
		{Track{Artists: []Artist{{Name: "A"}, {Name: "B"}}}, "A, B"},
	}
	for _, tt := range tests {
		if got := tt.track.ArtistNames(); got != tt.want {
			t.Errorf("ArtistNames() = %q, want %q", got, tt.want)
		}
	}
}
