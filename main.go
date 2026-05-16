package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	baseAPIURL      = "https://api.spotify.com/v1"
	refreshTokenURL = "https://accounts.spotify.com/api/token"
)

type AccessTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type SpotifyErrorResponse struct {
	Error struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

type LikedSongsSearchResponse struct {
	Total int         `json:"total"`
	Items []LikedSong `json:"items"`
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

func getAccessToken(clientID, clientSecret, refreshToken string) (string, error) {
	body := fmt.Sprintf("grant_type=refresh_token&refresh_token=%s", refreshToken)
	req, err := http.NewRequest("POST", refreshTokenURL, strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
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

	return tokenResponse.AccessToken, nil
}

func getLikedSongs(accessToken string) ([]Track, error) {
	req, err := http.NewRequest("GET", baseAPIURL+"/me/tracks?limit=50", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create liked songs request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
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

	var response LikedSongsSearchResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode liked songs: %w", err)
	}

	likedTracksForCurrentMonth := filterLikedSongsForCurrentMonth(response)
	log.Printf("Found %d liked song(s) for this month", len(likedTracksForCurrentMonth))

	return likedTracksForCurrentMonth, nil
}

func filterLikedSongsForCurrentMonth(likedSongs LikedSongsSearchResponse) []Track {
	var likedSongsForCurrentMonth []Track
	for _, song := range likedSongs.Items {
		if song.AddedAt.Month() == time.Now().Month() {
			likedSongsForCurrentMonth = append(likedSongsForCurrentMonth, song.Track)
		}
	}
	return likedSongsForCurrentMonth
}

func createPlaylist(accessToken, playlistName string) (string, error) {
	userID := "eduardohitek"
	payload := map[string]string{
		"name":        playlistName,
		"description": "Monthly Playlist",
		"public":      "false",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal playlist payload: %w", err)
	}

	req, err := http.NewRequest("POST", baseAPIURL+"/users/"+userID+"/playlists", bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create playlist request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
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

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to decode create playlist response: %w", err)
	}

	idRaw := result["id"]
	if idRaw == nil {
		return "", fmt.Errorf("failed to create playlist: no id in response")
	}

	return idRaw.(string), nil
}

func searchPlaylist(accessToken, playlistName string) (string, error) {
	req, err := http.NewRequest("GET", baseAPIURL+"/me/playlists?limit=50", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create search playlist request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
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

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to decode search playlist response: %w", err)
	}

	itemsRaw := result["items"]
	if itemsRaw == nil {
		return "", nil
	}

	playlists := itemsRaw.([]interface{})
	for _, playlist := range playlists {
		if playlist == nil {
			continue
		}
		pl := playlist.(map[string]interface{})
		nameRaw := pl["name"]
		idRaw := pl["id"]
		if nameRaw == nil || idRaw == nil {
			continue
		}
		if nameRaw.(string) == playlistName {
			return idRaw.(string), nil
		}
	}
	return "", nil
}

func addSongToPlaylist(accessToken, playlistID string, tracks []Track) error {
	for _, track := range tracks {
		log.Printf("Checking if track %q by %s is already in the playlist", track.Name, track.Artists[0].Name)
		exists, err := checkSongAlreadyInPlaylist(accessToken, playlistID, track.ID)
		if err != nil {
			return fmt.Errorf("failed to check track %q: %w", track.ID, err)
		}
		if !exists {
			log.Printf("Adding track %q by %s to the playlist", track.Name, track.Artists[0].Name)

			url := fmt.Sprintf("%s/playlists/%s/tracks?uris=spotify:track:%s", baseAPIURL, playlistID, track.ID)
			req, err := http.NewRequest("POST", url, nil)
			if err != nil {
				return fmt.Errorf("failed to create add track request: %w", err)
			}
			req.Header.Set("Authorization", "Bearer "+accessToken)
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("add track request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
				respBody, _ := io.ReadAll(resp.Body)
				var spotifyErr SpotifyErrorResponse
				json.Unmarshal(respBody, &spotifyErr)
				log.Printf("Add track endpoint returned %d: %s", resp.StatusCode, spotifyErr.Error.Message)
				return fmt.Errorf("add track endpoint returned status %d for track %q", resp.StatusCode, track.ID)
			}
		}
	}
	return nil
}

func checkSongAlreadyInPlaylist(accessToken, playlistID, trackID string) (bool, error) {
	var response struct {
		Items []struct {
			Track struct {
				ID string `json:"id"`
			} `json:"track"`
		} `json:"items"`
	}

	req, err := http.NewRequest("GET", baseAPIURL+"/playlists/"+playlistID+"/tracks?limit=100", nil)
	if err != nil {
		return false, fmt.Errorf("failed to create check tracks request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("check tracks request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read check tracks response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var spotifyErr SpotifyErrorResponse
		json.Unmarshal(body, &spotifyErr)
		log.Printf("Check tracks endpoint returned %d: %s", resp.StatusCode, spotifyErr.Error.Message)
		return false, fmt.Errorf("check tracks endpoint returned status %d", resp.StatusCode)
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return false, fmt.Errorf("failed to decode check tracks response: %w", err)
	}

	for _, item := range response.Items {
		if item.Track.ID == trackID {
			return true, nil
		}
	}
	return false, nil
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

	currentTime := time.Now()
	playlistName := fmt.Sprintf("%s'%d", currentTime.Format("Jan"), currentTime.Year()%100)
	log.Printf("Starting monthly playlist update for %q", playlistName)

	accessToken, err := getAccessToken(clientID, clientSecret, refreshToken)
	if err != nil {
		log.Fatalf("Error getting access token: %v", err)
	}
	log.Println("Access token obtained successfully")

	likedSongs, err := getLikedSongs(accessToken)
	if err != nil {
		log.Fatalf("Error getting liked songs: %v", err)
	}

	playlistID, err := searchPlaylist(accessToken, playlistName)
	if err != nil {
		log.Fatalf("Error searching playlist: %v", err)
	}

	if playlistID == "" {
		log.Printf("Playlist %q not found, creating it", playlistName)
		playlistID, err = createPlaylist(accessToken, playlistName)
		if err != nil {
			log.Fatalf("Error creating playlist: %v", err)
		}
		log.Printf("Playlist created with ID: %q", playlistID)
	} else {
		log.Printf("Found existing playlist with ID: %q", playlistID)
	}

	if len(likedSongs) > 0 {
		if err := addSongToPlaylist(accessToken, playlistID, likedSongs); err != nil {
			log.Fatalf("Error adding songs to playlist: %v", err)
		}
	}

	log.Printf("Done! Playlist %q updated successfully", playlistName)
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
