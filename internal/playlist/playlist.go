// Package playlist holds the monthly playlist rules and the update flow.
package playlist

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eduardohitek/spotify-cli/internal/spotify"
)

const (
	// TimeZone decides which month a liked song belongs to and the playlist name.
	TimeZone = "America/Sao_Paulo"

	Description = "Monthly Playlist"
)

// MonthStart returns the first instant of the month containing t, in t's location.
func MonthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// Name returns the playlist name for a month, like "Oct'26".
func Name(monthStart time.Time) string {
	return fmt.Sprintf("%s'%02d", monthStart.Format("Jan"), monthStart.Year()%100)
}

// TracksForMonth returns the tracks added during the month starting at monthStart.
func TracksForMonth(songs []spotify.SavedTrack, monthStart time.Time) []spotify.Track {
	monthEnd := monthStart.AddDate(0, 1, 0)
	var tracks []spotify.Track
	for _, song := range songs {
		if !song.AddedAt.Before(monthStart) && song.AddedAt.Before(monthEnd) {
			tracks = append(tracks, song.Track)
		}
	}
	return tracks
}

// NewTracks returns the tracks not in existing, without repeating a track.
func NewTracks(tracks []spotify.Track, existing map[string]bool) []spotify.Track {
	seen := make(map[string]bool, len(existing)+len(tracks))
	for id := range existing {
		seen[id] = true
	}
	var result []spotify.Track
	for _, track := range tracks {
		if seen[track.ID] {
			continue
		}
		seen[track.ID] = true
		result = append(result, track)
	}
	return result
}

// Client is the part of the Spotify API the Updater needs.
type Client interface {
	LikedSongsSince(ctx context.Context, since time.Time) ([]spotify.SavedTrack, error)
	FindPlaylist(ctx context.Context, name string) (*spotify.Playlist, error)
	CreatePlaylist(ctx context.Context, name, description string, public bool) (*spotify.Playlist, error)
	PlaylistTrackIDs(ctx context.Context, playlistID string) (map[string]bool, error)
	AddTracks(ctx context.Context, playlistID string, trackIDs []string) error
}

type Updater struct {
	Client Client
	Logger *slog.Logger
}

// Run updates the playlists of the month containing now and of the previous month, so songs
// liked after the previous month's last run are not lost. now's location decides the months.
func (u *Updater) Run(ctx context.Context, now time.Time) error {
	currentMonth := MonthStart(now)
	previousMonth := currentMonth.AddDate(0, -1, 0)

	songs, err := u.Client.LikedSongsSince(ctx, previousMonth)
	if err != nil {
		return err
	}

	for _, month := range []time.Time{previousMonth, currentMonth} {
		if err := u.updateMonth(ctx, month, songs); err != nil {
			return fmt.Errorf("updating %q: %w", Name(month), err)
		}
	}
	return nil
}

// updateMonth adds the month's liked songs to its playlist, creating the playlist only when
// there is something to add.
func (u *Updater) updateMonth(ctx context.Context, monthStart time.Time, songs []spotify.SavedTrack) error {
	name := Name(monthStart)
	tracks := TracksForMonth(songs, monthStart)
	logger := u.Logger.With("playlist", name)
	logger.Info("liked songs found", "count", len(tracks))
	if len(tracks) == 0 {
		return nil
	}

	playlist, err := u.Client.FindPlaylist(ctx, name)
	if err != nil {
		return err
	}

	existing := map[string]bool{}
	if playlist == nil {
		playlist, err = u.Client.CreatePlaylist(ctx, name, Description, false)
		if err != nil {
			return err
		}
		logger.Info("playlist created", "id", playlist.ID)
	} else {
		existing, err = u.Client.PlaylistTrackIDs(ctx, playlist.ID)
		if err != nil {
			return err
		}
	}

	toAdd := NewTracks(tracks, existing)
	ids := make([]string, len(toAdd))
	for i, track := range toAdd {
		ids[i] = track.ID
		logger.Info("adding track", "track", track.Name, "artists", track.ArtistNames())
	}
	if err := u.Client.AddTracks(ctx, playlist.ID, ids); err != nil {
		return err
	}
	logger.Info("playlist up to date", "added", len(toAdd))
	return nil
}
