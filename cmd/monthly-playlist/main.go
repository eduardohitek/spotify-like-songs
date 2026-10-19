// Command monthly-playlist copies the current and previous month's Spotify liked songs into
// monthly playlists such as "Oct'26".
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"
	_ "time/tzdata" // embed the time zone database so playlist.TimeZone loads on any runner

	"github.com/joho/godotenv"

	"github.com/eduardohitek/spotify-cli/internal/playlist"
	"github.com/eduardohitek/spotify-cli/internal/spotify"
)

const runTimeout = 5 * time.Minute

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(context.Background(), logger); err != nil {
		logger.Error("monthly playlist update failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	if err := loadEnvFile(".env.local"); err != nil {
		return err
	}

	clientID, clientSecret, refreshToken, err := credentials()
	if err != nil {
		return err
	}

	location, err := time.LoadLocation(playlist.TimeZone)
	if err != nil {
		return fmt.Errorf("loading time zone %q: %w", playlist.TimeZone, err)
	}

	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	client := spotify.NewClient()
	token, err := client.Authenticate(ctx, clientID, clientSecret, refreshToken)
	if err != nil {
		return err
	}
	// The new value is intentionally not logged: it would not be masked in CI logs.
	if token.RefreshToken != "" && token.RefreshToken != refreshToken {
		logger.Warn("Spotify rotated the refresh token; the current SPOTIFY_REFRESH_TOKEN may stop working. Regenerate it with `go run ./cmd/auth` and update the secret.")
	}

	updater := &playlist.Updater{Client: client, Logger: logger}
	if err := updater.Run(ctx, time.Now().In(location)); err != nil {
		return err
	}
	logger.Info("done")
	return nil
}

// loadEnvFile loads variables from path when it exists; CI provides them as env vars instead.
func loadEnvFile(path string) error {
	if err := godotenv.Load(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("loading %s: %w", path, err)
	}
	return nil
}

func credentials() (clientID, clientSecret, refreshToken string, err error) {
	var missing []string
	get := func(name string) string {
		value := os.Getenv(name)
		if value == "" {
			missing = append(missing, name)
		}
		return value
	}
	clientID = get("SPOTIFY_CLIENT_ID")
	clientSecret = get("SPOTIFY_CLIENT_SECRET")
	refreshToken = get("SPOTIFY_REFRESH_TOKEN")
	if len(missing) > 0 {
		return "", "", "", fmt.Errorf("missing environment variables: %v", missing)
	}
	return clientID, clientSecret, refreshToken, nil
}
