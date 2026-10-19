package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // embed the time zone database so playlistTimeZone loads on any runner

	"github.com/joho/godotenv"
)

const (
	baseAPIURL      = "https://api.spotify.com/v1"
	refreshTokenURL = "https://accounts.spotify.com/api/token"

	// playlistTimeZone decides which month a liked song belongs to and the playlist name.
	playlistTimeZone = "America/Sao_Paulo"

	// maxItemsPerAdd is the Spotify limit of items per "add items to playlist" request.
	maxItemsPerAdd = 100
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

type AccessTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type SpotifyErrorResponse struct {
	Error struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

type LikedSongsPage struct {
	Items []LikedSong `json:"items"`
	Next  string      `json:"next"`
}

type LikedSong struct {
	AddedAt time.Time `json:"added_at"`
	Track   Track     `json:"track"`
}

type Track struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Artists []Artist `json:"artists"`
}

type Artist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Playlist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PlaylistsPage struct {
	Items []*Playlist `json:"items"`
}

type PlaylistItemsPage struct {
	Items []struct {
		Item *struct {
			ID string `json:"id"`
		} `json:"item"`
	} `json:"items"`
	Next string `json:"next"`
}

type CreatePlaylistRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Public      bool   `json:"public"`
}

type AddItemsRequest struct {
	URIs []string `json:"uris"`
}

func getAccessToken(clientID, clientSecret, refreshToken string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	req, err := http.NewRequest("POST", refreshTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("Token endpoint returned %d: %s", resp.StatusCode, string(respBody))
		return "", fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var tokenResponse AccessTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	if tokenResponse.AccessToken == "" {
		return "", fmt.Errorf("received empty access token from Spotify")
	}

	// The new value is intentionally not logged: it would not be masked in CI logs.
	if tokenResponse.RefreshToken != "" && tokenResponse.RefreshToken != refreshToken {
		log.Printf("Warning: Spotify rotated the refresh token; the current SPOTIFY_REFRESH_TOKEN may stop working. Regenerate it with `go run ./cmd/auth` and update the secret.")
	}

	return tokenResponse.AccessToken, nil
}

// getLikedSongsSince returns liked songs added at or after since. Spotify lists them newest
// first, so pagination stops at the first older song.
func getLikedSongsSince(accessToken string, since time.Time) ([]LikedSong, error) {
	var songs []LikedSong
	pageURL := baseAPIURL + "/me/tracks?limit=50"
	for pageURL != "" {
		page, err := getLikedSongsPage(accessToken, pageURL)
		if err != nil {
			return nil, err
		}
		for _, song := range page.Items {
			if song.AddedAt.Before(since) {
				return songs, nil
			}
			songs = append(songs, song)
		}
		pageURL = page.Next
	}
	return songs, nil
}

func getLikedSongsPage(accessToken, pageURL string) (*LikedSongsPage, error) {
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create liked songs request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("liked songs request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read liked songs response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var spotifyErr SpotifyErrorResponse
		json.Unmarshal(body, &spotifyErr)
		log.Printf("Liked songs endpoint returned %d: %s", resp.StatusCode, spotifyErr.Error.Message)
		return nil, fmt.Errorf("liked songs endpoint returned status %d", resp.StatusCode)
	}

	var page LikedSongsPage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("failed to decode liked songs: %w", err)
	}
	return &page, nil
}

// filterLikedSongsForMonth returns the tracks added during the month starting at monthStart.
func filterLikedSongsForMonth(songs []LikedSong, monthStart time.Time) []Track {
	monthEnd := monthStart.AddDate(0, 1, 0)
	var tracks []Track
	for _, song := range songs {
		if !song.AddedAt.Before(monthStart) && song.AddedAt.Before(monthEnd) {
			tracks = append(tracks, song.Track)
		}
	}
	return tracks
}

func createPlaylist(accessToken, playlistName string) (string, error) {
	body, err := json.Marshal(CreatePlaylistRequest{
		Name:        playlistName,
		Description: "Monthly Playlist",
		Public:      false,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal playlist payload: %w", err)
	}

	req, err := http.NewRequest("POST", baseAPIURL+"/me/playlists", bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create playlist request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create playlist request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read create playlist response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		var spotifyErr SpotifyErrorResponse
		json.Unmarshal(respBody, &spotifyErr)
		log.Printf("Create playlist endpoint returned %d: %s", resp.StatusCode, spotifyErr.Error.Message)
		return "", fmt.Errorf("create playlist endpoint returned status %d", resp.StatusCode)
	}

	var playlist Playlist
	if err := json.Unmarshal(respBody, &playlist); err != nil {
		return "", fmt.Errorf("failed to decode create playlist response: %w", err)
	}
	if playlist.ID == "" {
		return "", fmt.Errorf("failed to create playlist: no id in response")
	}

	return playlist.ID, nil
}

func searchPlaylist(accessToken, playlistName string) (string, error) {
	req, err := http.NewRequest("GET", baseAPIURL+"/me/playlists?limit=50", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create search playlist request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search playlist request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read search playlist response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var spotifyErr SpotifyErrorResponse
		json.Unmarshal(respBody, &spotifyErr)
		log.Printf("Search playlist endpoint returned %d: %s", resp.StatusCode, spotifyErr.Error.Message)
		return "", fmt.Errorf("search playlist endpoint returned status %d", resp.StatusCode)
	}

	var page PlaylistsPage
	if err := json.Unmarshal(respBody, &page); err != nil {
		return "", fmt.Errorf("failed to decode search playlist response: %w", err)
	}

	for _, playlist := range page.Items {
		if playlist != nil && playlist.Name == playlistName {
			return playlist.ID, nil
		}
	}
	return "", nil
}

// getPlaylistTrackIDs returns the IDs of every track already in the playlist.
func getPlaylistTrackIDs(accessToken, playlistID string) (map[string]bool, error) {
	ids := make(map[string]bool)
	pageURL := baseAPIURL + "/playlists/" + playlistID + "/items?limit=50"
	for pageURL != "" {
		page, err := getPlaylistItemsPage(accessToken, pageURL)
		if err != nil {
			return nil, err
		}
		for _, entry := range page.Items {
			if entry.Item != nil {
				ids[entry.Item.ID] = true
			}
		}
		pageURL = page.Next
	}
	return ids, nil
}

func getPlaylistItemsPage(accessToken, pageURL string) (*PlaylistItemsPage, error) {
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create playlist items request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("playlist items request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read playlist items response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var spotifyErr SpotifyErrorResponse
		json.Unmarshal(body, &spotifyErr)
		log.Printf("Playlist items endpoint returned %d: %s", resp.StatusCode, spotifyErr.Error.Message)
		return nil, fmt.Errorf("playlist items endpoint returned status %d", resp.StatusCode)
	}

	var page PlaylistItemsPage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("failed to decode playlist items response: %w", err)
	}
	return &page, nil
}

func addTracksToPlaylist(accessToken, playlistID string, tracks []Track) error {
	for start := 0; start < len(tracks); start += maxItemsPerAdd {
		end := min(start+maxItemsPerAdd, len(tracks))
		uris := make([]string, 0, end-start)
		for _, track := range tracks[start:end] {
			uris = append(uris, "spotify:track:"+track.ID)
		}
		if err := addPlaylistItems(accessToken, playlistID, uris); err != nil {
			return err
		}
	}
	return nil
}

func addPlaylistItems(accessToken, playlistID string, uris []string) error {
	body, err := json.Marshal(AddItemsRequest{URIs: uris})
	if err != nil {
		return fmt.Errorf("failed to marshal add items payload: %w", err)
	}

	req, err := http.NewRequest("POST", baseAPIURL+"/playlists/"+playlistID+"/items", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create add items request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("add items request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		var spotifyErr SpotifyErrorResponse
		json.Unmarshal(respBody, &spotifyErr)
		log.Printf("Add items endpoint returned %d: %s", resp.StatusCode, spotifyErr.Error.Message)
		return fmt.Errorf("add items endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

// updateMonthlyPlaylist adds the month's liked songs to its playlist, creating the playlist
// only when there is something to add.
func updateMonthlyPlaylist(accessToken string, monthStart time.Time, likedSongs []LikedSong) error {
	playlistName := monthlyPlaylistName(monthStart)
	tracks := filterLikedSongsForMonth(likedSongs, monthStart)
	log.Printf("Found %d liked song(s) for %q", len(tracks), playlistName)

	playlistID, err := searchPlaylist(accessToken, playlistName)
	if err != nil {
		return fmt.Errorf("searching playlist %q: %w", playlistName, err)
	}

	if len(tracks) == 0 {
		return nil
	}

	existing := map[string]bool{}
	if playlistID == "" {
		log.Printf("Playlist %q not found, creating it", playlistName)
		playlistID, err = createPlaylist(accessToken, playlistName)
		if err != nil {
			return fmt.Errorf("creating playlist %q: %w", playlistName, err)
		}
		log.Printf("Playlist created with ID: %q", playlistID)
	} else {
		log.Printf("Found existing playlist %q with ID: %q", playlistName, playlistID)
		existing, err = getPlaylistTrackIDs(accessToken, playlistID)
		if err != nil {
			return fmt.Errorf("reading playlist %q: %w", playlistName, err)
		}
	}

	var toAdd []Track
	for _, track := range tracks {
		if existing[track.ID] {
			continue
		}
		existing[track.ID] = true
		log.Printf("Adding track %q by %s to %q", track.Name, artistNames(track), playlistName)
		toAdd = append(toAdd, track)
	}

	if err := addTracksToPlaylist(accessToken, playlistID, toAdd); err != nil {
		return fmt.Errorf("adding tracks to %q: %w", playlistName, err)
	}
	log.Printf("Playlist %q is up to date (%d track(s) added)", playlistName, len(toAdd))
	return nil
}

func monthlyPlaylistName(monthStart time.Time) string {
	return fmt.Sprintf("%s'%02d", monthStart.Format("Jan"), monthStart.Year()%100)
}

func artistNames(track Track) string {
	names := make([]string, 0, len(track.Artists))
	for _, artist := range track.Artists {
		names = append(names, artist.Name)
	}
	if len(names) == 0 {
		return "unknown artist"
	}
	return strings.Join(names, ", ")
}

func main() {
	loadEnvFile()

	clientID := os.Getenv("SPOTIFY_CLIENT_ID")
	clientSecret := os.Getenv("SPOTIFY_CLIENT_SECRET")
	refreshToken := os.Getenv("SPOTIFY_REFRESH_TOKEN")

	if clientID == "" {
		log.Fatal("SPOTIFY_CLIENT_ID is not set")
	}
	if clientSecret == "" {
		log.Fatal("SPOTIFY_CLIENT_SECRET is not set")
	}
	if refreshToken == "" {
		log.Fatal("SPOTIFY_REFRESH_TOKEN is not set")
	}

	location, err := time.LoadLocation(playlistTimeZone)
	if err != nil {
		log.Fatalf("Error loading time zone %q: %v", playlistTimeZone, err)
	}
	now := time.Now().In(location)
	currentMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
	// The previous month is processed too, so songs liked after its last run are not lost.
	previousMonth := currentMonth.AddDate(0, -1, 0)
	log.Printf("Starting monthly playlist update for %q and %q", monthlyPlaylistName(previousMonth), monthlyPlaylistName(currentMonth))

	accessToken, err := getAccessToken(clientID, clientSecret, refreshToken)
	if err != nil {
		log.Fatalf("Error getting access token: %v", err)
	}
	log.Println("Access token obtained successfully")

	likedSongs, err := getLikedSongsSince(accessToken, previousMonth)
	if err != nil {
		log.Fatalf("Error getting liked songs: %v", err)
	}

	for _, month := range []time.Time{previousMonth, currentMonth} {
		if err := updateMonthlyPlaylist(accessToken, month, likedSongs); err != nil {
			log.Fatalf("Error updating playlist: %v", err)
		}
	}

	log.Println("Done!")
}

func loadEnvFile() {
	file, err := os.Stat(".env.local")
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		panic(err)
	}

	if err := godotenv.Load(file.Name()); err != nil {
		panic(err)
	}
}
