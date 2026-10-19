// Package spotify is a minimal client for the parts of the Spotify Web API this project uses.
package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultAPIURL   = "https://api.spotify.com/v1"
	DefaultTokenURL = "https://accounts.spotify.com/api/token"

	// maxItemsPerAdd is the Spotify limit of items per "add items to playlist" request.
	maxItemsPerAdd = 100
	pageLimit      = 50
)

// Client calls the Spotify Web API. Use NewClient and then Authenticate before API calls.
type Client struct {
	APIURL     string
	TokenURL   string
	HTTPClient *http.Client

	accessToken string
}

func NewClient() *Client {
	return &Client{
		APIURL:     DefaultAPIURL,
		TokenURL:   DefaultTokenURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError is a non-2xx response from Spotify.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("spotify returned status %d: %s", e.StatusCode, e.Message)
}

type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type Artist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Track struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Artists []Artist `json:"artists"`
}

// ArtistNames returns the artists joined by commas, or "unknown artist" when there are none.
func (t Track) ArtistNames() string {
	names := make([]string, 0, len(t.Artists))
	for _, artist := range t.Artists {
		names = append(names, artist.Name)
	}
	if len(names) == 0 {
		return "unknown artist"
	}
	return strings.Join(names, ", ")
}

type SavedTrack struct {
	AddedAt time.Time `json:"added_at"`
	Track   Track     `json:"track"`
}

type Playlist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Authenticate exchanges the refresh token for an access token used by later calls. The
// returned token carries a new refresh token when Spotify rotated it.
func (c *Client) Authenticate(ctx context.Context, clientID, clientSecret, refreshToken string) (*Token, error) {
	token, err := c.requestToken(ctx, clientID, clientSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		return nil, fmt.Errorf("refreshing access token: %w", err)
	}
	c.accessToken = token.AccessToken
	return token, nil
}

// ExchangeCode trades an authorization code for tokens (Authorization Code flow).
func (c *Client) ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*Token, error) {
	token, err := c.requestToken(ctx, clientID, clientSecret, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	})
	if err != nil {
		return nil, fmt.Errorf("exchanging authorization code: %w", err)
	}
	return token, nil
}

func (c *Client) requestToken(ctx context.Context, clientID, clientSecret string, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var token Token
	if err := c.send(req, &token); err != nil {
		return nil, err
	}
	if token.AccessToken == "" {
		return nil, fmt.Errorf("empty access token in response")
	}
	return &token, nil
}

// LikedSongsSince returns the liked songs added at or after since, newest first. Spotify lists
// them newest first, so pagination stops at the first older song.
func (c *Client) LikedSongsSince(ctx context.Context, since time.Time) ([]SavedTrack, error) {
	var songs []SavedTrack
	next := fmt.Sprintf("%s/me/tracks?limit=%d", c.APIURL, pageLimit)
	for next != "" {
		var page struct {
			Items []SavedTrack `json:"items"`
			Next  string       `json:"next"`
		}
		if err := c.get(ctx, next, &page); err != nil {
			return nil, fmt.Errorf("listing liked songs: %w", err)
		}
		for _, song := range page.Items {
			if song.AddedAt.Before(since) {
				return songs, nil
			}
			songs = append(songs, song)
		}
		next = page.Next
	}
	return songs, nil
}

// FindPlaylist returns the current user's playlist with the given name, or nil if there is none.
func (c *Client) FindPlaylist(ctx context.Context, name string) (*Playlist, error) {
	next := fmt.Sprintf("%s/me/playlists?limit=%d", c.APIURL, pageLimit)
	for next != "" {
		var page struct {
			Items []*Playlist `json:"items"`
			Next  string      `json:"next"`
		}
		if err := c.get(ctx, next, &page); err != nil {
			return nil, fmt.Errorf("listing playlists: %w", err)
		}
		for _, playlist := range page.Items {
			if playlist != nil && playlist.Name == name {
				return playlist, nil
			}
		}
		next = page.Next
	}
	return nil, nil
}

func (c *Client) CreatePlaylist(ctx context.Context, name, description string, public bool) (*Playlist, error) {
	body := struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Public      bool   `json:"public"`
	}{name, description, public}

	var playlist Playlist
	if err := c.doJSON(ctx, http.MethodPost, c.APIURL+"/me/playlists", body, &playlist); err != nil {
		return nil, fmt.Errorf("creating playlist %q: %w", name, err)
	}
	if playlist.ID == "" {
		return nil, fmt.Errorf("creating playlist %q: no id in response", name)
	}
	return &playlist, nil
}

// PlaylistTrackIDs returns the IDs of every track already in the playlist.
func (c *Client) PlaylistTrackIDs(ctx context.Context, playlistID string) (map[string]bool, error) {
	ids := make(map[string]bool)
	next := fmt.Sprintf("%s/playlists/%s/items?limit=%d", c.APIURL, url.PathEscape(playlistID), pageLimit)
	for next != "" {
		var page struct {
			Items []struct {
				Item *struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"items"`
			Next string `json:"next"`
		}
		if err := c.get(ctx, next, &page); err != nil {
			return nil, fmt.Errorf("listing playlist items: %w", err)
		}
		for _, entry := range page.Items {
			if entry.Item != nil {
				ids[entry.Item.ID] = true
			}
		}
		next = page.Next
	}
	return ids, nil
}

// AddTracks appends the tracks to the playlist, in batches of up to maxItemsPerAdd.
func (c *Client) AddTracks(ctx context.Context, playlistID string, trackIDs []string) error {
	endpoint := fmt.Sprintf("%s/playlists/%s/items", c.APIURL, url.PathEscape(playlistID))
	for start := 0; start < len(trackIDs); start += maxItemsPerAdd {
		batch := trackIDs[start:min(start+maxItemsPerAdd, len(trackIDs))]
		uris := make([]string, len(batch))
		for i, id := range batch {
			uris[i] = "spotify:track:" + id
		}
		body := struct {
			URIs []string `json:"uris"`
		}{uris}
		if err := c.doJSON(ctx, http.MethodPost, endpoint, body, nil); err != nil {
			return fmt.Errorf("adding tracks: %w", err)
		}
	}
	return nil
}

func (c *Client) get(ctx context.Context, endpoint string, out any) error {
	return c.doJSON(ctx, http.MethodGet, endpoint, nil, out)
}

// doJSON sends an authenticated API request with an optional JSON body and decodes the JSON
// response into out when out is not nil.
func (c *Client) doJSON(ctx context.Context, method, endpoint string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		reqBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, out)
}

func (c *Client) send(req *http.Request, out any) error {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{StatusCode: resp.StatusCode, Message: errorMessage(respBody)}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// errorMessage extracts the message from either error format Spotify uses: the Web API's
// {"error":{"message":...}} or the accounts service's {"error":...,"error_description":...}.
func errorMessage(body []byte) string {
	var apiErr struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error.Message != "" {
		return apiErr.Error.Message
	}

	var oauthErr struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(body, &oauthErr) == nil && oauthErr.Error != "" {
		if oauthErr.Description == "" {
			return oauthErr.Error
		}
		return oauthErr.Error + ": " + oauthErr.Description
	}

	return strings.TrimSpace(string(body))
}
