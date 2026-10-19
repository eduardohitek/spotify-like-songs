package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	if err := loadEnvFile(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Errorf("missing file: %v, want nil", err)
	}

	path := filepath.Join(t.TempDir(), ".env.local")
	if err := os.WriteFile(path, []byte("SPOTIFY_TEST_VAR=loaded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPOTIFY_TEST_VAR", "")
	os.Unsetenv("SPOTIFY_TEST_VAR")
	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("SPOTIFY_TEST_VAR"); got != "loaded" {
		t.Errorf("SPOTIFY_TEST_VAR = %q, want loaded", got)
	}
}

func TestCredentialsReportsAllMissing(t *testing.T) {
	t.Setenv("SPOTIFY_CLIENT_ID", "id")
	t.Setenv("SPOTIFY_CLIENT_SECRET", "")
	t.Setenv("SPOTIFY_REFRESH_TOKEN", "")

	_, _, _, err := credentials()
	if err == nil {
		t.Fatal("want an error")
	}
	for _, name := range []string{"SPOTIFY_CLIENT_SECRET", "SPOTIFY_REFRESH_TOKEN"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("err = %q, want it to mention %s", err, name)
		}
	}
	if strings.Contains(err.Error(), "SPOTIFY_CLIENT_ID") {
		t.Errorf("err = %q mentions a variable that is set", err)
	}
}
