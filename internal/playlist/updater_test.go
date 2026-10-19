package playlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eduardohitek/spotify-cli/internal/spotify"
)

// fakeSpotify is an in-memory Spotify Web API serving small pages, so pagination is exercised.
type fakeSpotify struct {
	t        *testing.T
	url      string
	pageSize int
	failAdd  bool // answer "add items" with 403

	mu        sync.Mutex
	liked     []spotify.SavedTrack // newest first, like Spotify
	playlists []*fakePlaylist
}

type fakePlaylist struct {
	ID     string
	Name   string
	Public bool
	Tracks []string
}

func newFakeSpotify(t *testing.T) *fakeSpotify {
	f := &fakeSpotify{t: t, pageSize: 2}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /me/tracks", f.listLiked)
	mux.HandleFunc("GET /me/playlists", f.listPlaylists)
	mux.HandleFunc("POST /me/playlists", f.createPlaylist)
	mux.HandleFunc("GET /playlists/{id}/items", f.listItems)
	mux.HandleFunc("POST /playlists/{id}/items", f.addItems)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f
}

func (f *fakeSpotify) client() *spotify.Client {
	c := spotify.NewClient()
	c.APIURL = f.url
	return c
}

func (f *fakeSpotify) playlist(name string) *fakePlaylist {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.playlists {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// page writes items[offset:offset+pageSize] with a next link when more remain.
func page[T any](f *fakeSpotify, w http.ResponseWriter, r *http.Request, items []T) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	end := min(offset+f.pageSize, len(items))
	body := map[string]any{"items": items[min(offset, end):end], "next": nil}
	if end < len(items) {
		body["next"] = fmt.Sprintf("%s%s?offset=%d", f.url, r.URL.Path, end)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
}

func (f *fakeSpotify) listLiked(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	page(f, w, r, f.liked)
}

func (f *fakeSpotify) listPlaylists(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	summaries := make([]spotify.Playlist, len(f.playlists))
	for i, p := range f.playlists {
		summaries[i] = spotify.Playlist{ID: p.ID, Name: p.Name}
	}
	page(f, w, r, summaries)
}

func (f *fakeSpotify) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		Public *bool  `json:"public"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Public == nil {
		f.t.Errorf("create playlist body: %v (public must be a boolean)", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	p := &fakePlaylist{ID: fmt.Sprintf("pl%d", len(f.playlists)+1), Name: body.Name, Public: *body.Public}
	f.playlists = append(f.playlists, p)
	f.mu.Unlock()
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(spotify.Playlist{ID: p.ID, Name: p.Name})
}

func (f *fakeSpotify) findByID(w http.ResponseWriter, id string) *fakePlaylist {
	for _, p := range f.playlists {
		if p.ID == id {
			return p
		}
	}
	w.WriteHeader(http.StatusNotFound)
	io.WriteString(w, `{"error":{"status":404,"message":"Not found"}}`)
	return nil
}

func (f *fakeSpotify) listItems(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.findByID(w, r.PathValue("id"))
	if p == nil {
		return
	}
	type item struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	items := make([]item, len(p.Tracks))
	for i, id := range p.Tracks {
		items[i].Item.ID = id
	}
	page(f, w, r, items)
}

func (f *fakeSpotify) addItems(w http.ResponseWriter, r *http.Request) {
	if f.failAdd {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"status":403,"message":"Forbidden"}}`)
		return
	}
	var body struct {
		URIs []string `json:"uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.URIs) == 0 || len(body.URIs) > 100 {
		f.t.Errorf("add items body: %v, %d uris", err, len(body.URIs))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.findByID(w, r.PathValue("id"))
	if p == nil {
		return
	}
	for _, uri := range body.URIs {
		p.Tracks = append(p.Tracks, strings.TrimPrefix(uri, "spotify:track:"))
	}
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, `{"snapshot_id":"s"}`)
}

func liked(t *testing.T, id, addedAt string) spotify.SavedTrack {
	at, err := time.Parse(time.RFC3339, addedAt)
	if err != nil {
		t.Fatal(err)
	}
	return spotify.SavedTrack{AddedAt: at, Track: spotify.Track{ID: id, Name: id}}
}

func newUpdater(f *fakeSpotify) *Updater {
	return &Updater{Client: f.client(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestUpdaterRun(t *testing.T) {
	f := newFakeSpotify(t)
	f.liked = []spotify.SavedTrack{
		liked(t, "oct-2", "2026-10-18T12:00:00Z"),
		liked(t, "oct-1", "2026-10-01T03:00:00Z"),
		liked(t, "sep-late", "2026-10-01T02:00:00Z"), // Sep 30, 23:00 in São Paulo
		liked(t, "sep-1", "2026-09-10T12:00:00Z"),
		liked(t, "aug-1", "2026-08-20T12:00:00Z"),
	}
	f.playlists = []*fakePlaylist{
		{ID: "other", Name: "Road trip"},
		{ID: "old", Name: "Aug'26"},
		{ID: "oct", Name: "Oct'26", Tracks: []string{"oct-1"}},
	}
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, mustLocation(t))
	u := newUpdater(f)

	for run := 1; run <= 2; run++ {
		if err := u.Run(context.Background(), now); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}

		oct := f.playlist("Oct'26")
		if got := strings.Join(oct.Tracks, ","); got != "oct-1,oct-2" {
			t.Errorf("run %d: Oct'26 = %s, want oct-1,oct-2", run, got)
		}
		sep := f.playlist("Sep'26")
		if sep == nil {
			t.Fatalf("run %d: Sep'26 not created", run)
		}
		if sep.Public {
			t.Errorf("run %d: Sep'26 created public", run)
		}
		if got := strings.Join(sep.Tracks, ","); got != "sep-late,sep-1" {
			t.Errorf("run %d: Sep'26 = %s, want sep-late,sep-1", run, got)
		}
		if got := f.playlist("Aug'26").Tracks; len(got) != 0 {
			t.Errorf("run %d: Aug'26 changed: %v", run, got)
		}
		if len(f.playlists) != 4 {
			t.Errorf("run %d: %d playlists, want 4", run, len(f.playlists))
		}
	}
}

func TestUpdaterRunWithoutSongsCreatesNothing(t *testing.T) {
	f := newFakeSpotify(t)
	f.liked = []spotify.SavedTrack{liked(t, "old", "2026-07-01T12:00:00Z")}
	now := time.Date(2026, 10, 1, 0, 30, 0, 0, mustLocation(t))

	if err := newUpdater(f).Run(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(f.playlists) != 0 {
		t.Errorf("created playlists: %+v", f.playlists)
	}
}

func TestUpdaterRunReportsAPIErrors(t *testing.T) {
	f := newFakeSpotify(t)
	f.failAdd = true
	f.liked = []spotify.SavedTrack{liked(t, "oct-1", "2026-10-10T12:00:00Z")}
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, mustLocation(t))

	err := newUpdater(f).Run(context.Background(), now)
	var apiErr *spotify.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("err = %v, want a 403 *spotify.APIError", err)
	}
	if !strings.Contains(err.Error(), "Oct'26") {
		t.Errorf("err = %q, want it to name the playlist", err)
	}
}
