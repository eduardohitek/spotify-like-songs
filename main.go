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

// Spotify API base URLs
const (
	baseAPIURL      = "https://api.spotify.com/v1"
	refreshTokenURL = "https://accounts.spotify.com/api/token"
)

// Struct for response data
type AccessTokenResponse struct {
	AccessToken string `json:"access_token"`
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

// Function to get a new access token using the refresh token
func getAccessToken(clientID, clientSecret, refreshToken string) (string, error) {
	req, err := http.NewRequest("POST", refreshTokenURL, strings.NewReader(fmt.Sprintf("grant_type=refresh_token&refresh_token=%s", refreshToken)))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var tokenResponse AccessTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return "", err
	}

	return tokenResponse.AccessToken, nil
}

// Function to get liked songs
func getLikedSongs(accessToken string) ([]Track, error) {
	var response LikedSongsSearchResponse
	req, _ := http.NewRequest("GET", baseAPIURL+"/me/tracks?limit=50", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, err
	}
	likedTrackforCurrentMonth := filterLikedSongsForCurrentMonth(response)

	log.Printf("Were found %d liked song(s) for this month", len(likedTrackforCurrentMonth))

	return likedTrackforCurrentMonth, nil
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

// Function to create a playlist
func createPlaylist(accessToken string, playlistName string) (string, error) {
	userID := "eduardohitek" // Replace with your Spotify User ID
	payload := map[string]string{
		"name":        playlistName,
		"description": "Monthly Playlist",
		"public":      "false",
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", baseAPIURL+"/users/"+userID+"/playlists", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	playlistID := result["id"].(string)

	return playlistID, nil
}

// Function to search for an existing playlist
func searchPlaylist(accessToken, playlistName string) (string, error) {
	req, _ := http.NewRequest("GET", baseAPIURL+"/me/playlists?limit=50", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	playlists := result["items"].([]interface{})
	for _, playlist := range playlists {
		pl := playlist.(map[string]interface{})
		if pl["name"].(string) == playlistName {
			return pl["id"].(string), nil
		}
	}
	return "", nil
}

// Function to add a song to a playlist
func addSongToPlaylist(accessToken, playlistID string, tracks []Track) error {
	for _, track := range tracks {
		log.Printf("Checking if the track %s by %s is already in the playlist.\n", track.Name, track.Artists[0].Name)
		exists, err := checkSongAlreadyInPlaylist(accessToken, playlistID, track.ID)
		if err != nil {
			return err
		}
		if !exists {
			log.Printf("Adding the track %s by %s to the playlist.\n", track.Name, track.Artists[0].Name)

			req, _ := http.NewRequest("POST", baseAPIURL+"/playlists/"+playlistID+"/tracks?uris=spotify:track:"+track.ID, nil)
			req.Header.Set("Authorization", "Bearer "+accessToken)
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{}
			_, err := client.Do(req)
			if err != nil {
				log.Println(err)
				return err
			}
		}
	}
	return nil
}

func checkSongAlreadyInPlaylist(accessToken, playListID, trackID string) (bool, error) {

	var response struct {
		Items []struct {
			Track struct {
				ID string `json:"id"`
			} `json:"track"`
		} `json:"items"`
	}

	req, _ := http.NewRequest("GET", baseAPIURL+"/playlists/"+playListID+"/tracks?limit=100", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	err = json.Unmarshal(body, &response)
	if err != nil {
		return false, err
	}

	for _, item := range response.Items {
		if item.Track.ID == trackID {
			return true, nil
		}
	}
	return false, nil

}

func main() {

	clientID := os.Getenv("SPOTIFY_CLIENT_ID")
	clientSecret := os.Getenv("SPOTIFY_CLIENT_SECRET")
	refreshToken := os.Getenv("SPOTIFY_REFRESH_TOKEN")
	// Get the current month and year for playlist naming
	currentTime := time.Now()
	playlistName := fmt.Sprintf("%s'%d", currentTime.Format("Jan"), currentTime.Year()%100)

	// Get access token
	accessToken, err := getAccessToken(clientID, clientSecret, refreshToken)
	if err != nil {
		fmt.Println("Error getting access token:", err)
		return
	}

	// Get the latest liked song
	likedSongs, err := getLikedSongs(accessToken)
	if err != nil {
		fmt.Println("Error getting liked songs:", err)
		return
	}

	// Check if the playlist exists
	playlistID, err := searchPlaylist(accessToken, playlistName)
	if err != nil {
		fmt.Println("Error searching playlist:", err)
		return
	}

	// If playlist doesn't exist, create it
	if playlistID == "" {
		playlistID, err = createPlaylist(accessToken, playlistName)
		if err != nil {
			fmt.Println("Error creating playlist:", err)
			return
		}
	}

	// Add the liked song to the playlist
	err = addSongToPlaylist(accessToken, playlistID, likedSongs)
	if err != nil {
		fmt.Println("Error adding song to playlist:", err)
		return
	}

	fmt.Println("Song added to playlist:", playlistName)
}

func loadEnvFile() {
	file, err := os.Stat(".env.local")
	if err != nil {
		if !os.IsNotExist(err) {
			panic(err)
		}

		return
	}

	if err := godotenv.Load(file.Name()); err != nil {
		panic(err)
	}
}
