package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/joho/godotenv"
)

const (
	authorizeURL = "https://accounts.spotify.com/authorize"
	tokenURL     = "https://accounts.spotify.com/api/token"
	redirectURI  = "http://127.0.0.1:8080/callback"
	scopes       = "user-library-read playlist-read-private playlist-modify-private playlist-modify-public"
)

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

func main() {
	godotenv.Load(".env.local")

	clientID := os.Getenv("SPOTIFY_CLIENT_ID")
	clientSecret := os.Getenv("SPOTIFY_CLIENT_SECRET")

	if clientID == "" || clientSecret == "" {
		log.Fatal("SPOTIFY_CLIENT_ID and SPOTIFY_CLIENT_SECRET must be set in .env.local")
	}

	state := randomString(16)

	authParams := url.Values{}
	authParams.Set("client_id", clientID)
	authParams.Set("response_type", "code")
	authParams.Set("redirect_uri", redirectURI)
	authParams.Set("scope", scopes)
	authParams.Set("state", state)

	authURL := authorizeURL + "?" + authParams.Encode()

	fmt.Println("Opening browser to authorize with Spotify...")
	fmt.Printf("\nIf the browser doesn't open, visit this URL:\n%s\n\n", authURL)

	openBrowser(authURL)

	codeChan := make(chan string)
	errChan := make(chan error)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "State mismatch. Please try again.")
			errChan <- fmt.Errorf("state mismatch")
			return
		}

		errorParam := r.URL.Query().Get("error")
		if errorParam != "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Authorization error: %s", errorParam)
			errChan <- fmt.Errorf("authorization error: %s", errorParam)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "No authorization code received.")
			errChan <- fmt.Errorf("no authorization code received")
			return
		}

		fmt.Fprint(w, "Authorization successful! You can close this window and return to the terminal.")
		codeChan <- code
	})

	server := &http.Server{Addr: "127.0.0.1:8080", Handler: mux}

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	var code string
	select {
	case code = <-codeChan:
	case err := <-errChan:
		server.Close()
		log.Fatal(err)
	}

	server.Close()

	fmt.Println("Exchanging authorization code for tokens...")

	tokens, err := exchangeCodeForToken(clientID, clientSecret, code)
	if err != nil {
		log.Fatalf("Failed to exchange code: %v", err)
	}

	fmt.Println("\nToken exchange successful!")
	fmt.Printf("\nAdd this to .env.local:\n")
	fmt.Printf("SPOTIFY_REFRESH_TOKEN=%s\n", tokens.RefreshToken)
}

func base64URLEncode(b []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64URLEncode(b)
}

// Uses the confidential Authorization Code flow (Basic auth) so the refresh token matches how
// main.go refreshes it and is not rotated on every use, as PKCE tokens are.
func exchangeCodeForToken(clientID, clientSecret, code string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokens TokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if tokens.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh_token in response. Did you already authorize this app? Try removing the app from https://www.spotify.com/account/apps/ and run again")
	}

	return &tokens, nil
}

func openBrowser(urlStr string) {
	exec.Command("open", urlStr).Start()
}
