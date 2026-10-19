// Command auth runs the Spotify Authorization Code flow once and prints a refresh token for
// SPOTIFY_REFRESH_TOKEN.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/joho/godotenv"

	"github.com/eduardohitek/spotify-cli/internal/spotify"
)

const (
	authorizeURL = "https://accounts.spotify.com/authorize"
	redirectURI  = "http://127.0.0.1:8080/callback"
	scopes       = "user-library-read playlist-read-private playlist-modify-private playlist-modify-public"
)

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

	// Buffered so the first callback never blocks; later callbacks are ignored.
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)
	report := func(code string, err error) {
		if err != nil {
			select {
			case errChan <- err:
			default:
			}
			return
		}
		select {
		case codeChan <- code:
		default:
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "State mismatch. Please try again.")
			report("", fmt.Errorf("state mismatch"))
			return
		}

		errorParam := r.URL.Query().Get("error")
		if errorParam != "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Authorization error: %s", errorParam)
			report("", fmt.Errorf("authorization error: %s", errorParam))
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "No authorization code received.")
			report("", fmt.Errorf("no authorization code received"))
			return
		}

		fmt.Fprint(w, "Authorization successful! You can close this window and return to the terminal.")
		report(code, nil)
	})

	server := &http.Server{Addr: "127.0.0.1:8080", Handler: mux}

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	var code string
	select {
	case err := <-errChan:
		server.Close()
		log.Fatal(err)
	case code = <-codeChan:
	}

	server.Close()

	fmt.Println("Exchanging authorization code for tokens...")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	tokens, err := spotify.NewClient().ExchangeCode(ctx, clientID, clientSecret, code, redirectURI)
	if err != nil {
		log.Fatalf("Failed to exchange code: %v", err)
	}
	if tokens.RefreshToken == "" {
		log.Fatal("No refresh_token in response.")
	}

	fmt.Println("\nToken exchange successful!")
	fmt.Printf("\nAdd this to .env.local:\n")
	fmt.Printf("SPOTIFY_REFRESH_TOKEN=%s\n", tokens.RefreshToken)
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func openBrowser(urlStr string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", urlStr)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", urlStr)
	default:
		cmd = exec.Command("xdg-open", urlStr)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("Could not open the browser: %v", err)
	}
}
