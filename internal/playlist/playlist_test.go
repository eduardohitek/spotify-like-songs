package playlist

import (
	"testing"
	"time"

	"github.com/eduardohitek/spotify-cli/internal/spotify"
)

func mustLocation(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(TimeZone)
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func TestMonthStartAndName(t *testing.T) {
	location := mustLocation(t)
	tests := []struct {
		now  time.Time
		want string
	}{
		{time.Date(2026, 10, 19, 15, 0, 0, 0, location), "Oct'26"},
		{time.Date(2026, 10, 1, 0, 0, 0, 0, location), "Oct'26"},
		{time.Date(2026, 9, 30, 23, 59, 59, 0, location), "Sep'26"},
		{time.Date(2026, 1, 1, 0, 0, 0, 0, location).AddDate(0, -1, 0), "Dec'25"},
		{time.Date(2005, 3, 10, 0, 0, 0, 0, location), "Mar'05"},
	}
	for _, tt := range tests {
		start := MonthStart(tt.now)
		if start.Day() != 1 || start.Hour() != 0 || start.Location() != location {
			t.Errorf("MonthStart(%v) = %v", tt.now, start)
		}
		if got := Name(start); got != tt.want {
			t.Errorf("Name(MonthStart(%v)) = %q, want %q", tt.now, got, tt.want)
		}
	}
}

func TestTracksForMonth(t *testing.T) {
	location := mustLocation(t)
	october := time.Date(2026, 10, 1, 0, 0, 0, 0, location)
	september := october.AddDate(0, -1, 0)
	utc := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	songs := []spotify.SavedTrack{
		{AddedAt: utc("2026-11-01T03:00:00Z"), Track: spotify.Track{ID: "next-month"}},
		{AddedAt: utc("2026-10-01T03:00:00Z"), Track: spotify.Track{ID: "oct-first-instant"}},
		{AddedAt: utc("2026-10-01T02:59:59Z"), Track: spotify.Track{ID: "sep-last-second-brt"}},
		{AddedAt: utc("2026-09-01T03:00:00Z"), Track: spotify.Track{ID: "sep-first-instant"}},
		{AddedAt: utc("2025-10-15T12:00:00Z"), Track: spotify.Track{ID: "last-year-october"}},
	}

	tests := []struct {
		month time.Time
		want  []string
	}{
		{october, []string{"oct-first-instant"}},
		{september, []string{"sep-last-second-brt", "sep-first-instant"}},
		{october.AddDate(0, -2, 0), nil},
	}
	for _, tt := range tests {
		got := TracksForMonth(songs, tt.month)
		if len(got) != len(tt.want) {
			t.Fatalf("TracksForMonth(%s) = %+v, want %v", Name(tt.month), got, tt.want)
		}
		for i := range got {
			if got[i].ID != tt.want[i] {
				t.Errorf("TracksForMonth(%s)[%d] = %q, want %q", Name(tt.month), i, got[i].ID, tt.want[i])
			}
		}
	}
}

func TestNewTracks(t *testing.T) {
	tracks := []spotify.Track{{ID: "a"}, {ID: "b"}, {ID: "a"}, {ID: "c"}}
	tests := []struct {
		name     string
		existing map[string]bool
		want     []string
	}{
		{"empty playlist", nil, []string{"a", "b", "c"}},
		{"some present", map[string]bool{"b": true}, []string{"a", "c"}},
		{"all present", map[string]bool{"a": true, "b": true, "c": true}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewTracks(tracks, tt.existing)
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i].ID != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i].ID, tt.want[i])
				}
			}
		})
	}
}
