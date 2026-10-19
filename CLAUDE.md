# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A single-binary Go job that copies the current month's Spotify liked songs into a private playlist named like `Oct'26` (`Jan'YY` format), creating the playlist if needed. It runs daily in GitHub Actions (`.github/workflows/spotify-automation.yaml`). The GitHub repo is `eduardohitek/spotify-like-songs`, although the Go module is `github.com/eduardohitek/spotify-cli`.

## Commands

```bash
go run main.go            # run the job (reads .env.local if present, else env vars)
go run ./cmd/auth         # one-off OAuth flow that prints a new SPOTIFY_REFRESH_TOKEN
go build ./... && go vet ./...
gofmt -l .
```

There are no tests.

CI:
```bash
gh workflow run "Spotify Automation" [--ref <branch>]
gh run watch
gh secret set SPOTIFY_REFRESH_TOKEN
```

## Configuration

`SPOTIFY_CLIENT_ID`, `SPOTIFY_CLIENT_SECRET` and `SPOTIFY_REFRESH_TOKEN` come from `.env.local` locally (untracked; `.env.example` is the template) and from repository secrets in CI.

## Architecture

- `main.go` — the whole job, plain `net/http` calls to the Spotify Web API, no SDK. Flow in `main()`: refresh token → access token, fetch liked songs, find playlist by name (or create it), add each track not already present.
- `cmd/auth/main.go` — local helper: opens the browser for the Authorization Code flow, listens on `http://127.0.0.1:8080/callback` (must be a registered redirect URI in the Spotify app), and prints the refresh token.

## Things to know before changing code

- **Token flows must match.** `cmd/auth` obtains the token with the confidential Authorization Code flow (Basic auth) and `main.go` refreshes it the same way. Do not switch `cmd/auth` back to PKCE: PKCE refresh tokens rotate, and the CI secret ends up revoked (`invalid_grant: Refresh token revoked`).
- **One refresh token per environment.** Use a separate token locally from the one stored in the `SPOTIFY_REFRESH_TOKEN` secret. If `main.go` logs that Spotify rotated the token, the secret needs regenerating; the new value is deliberately never logged because CI would not mask it.
- **Pagination limits.** Only the 50 most recent liked songs, the first 50 user playlists, and the first 100 tracks of the playlist are read. Past those limits, songs can be missed, a duplicate monthly playlist can be created, or tracks can be re-added.
- `filterLikedSongsForCurrentMonth` compares only the month, not the year.
- The playlist owner is hardcoded as `userID := "eduardohitek"` in `createPlaylist`.
- The workflow has a `keepalive` job that re-enables itself via the API, because GitHub disables scheduled workflows after 60 days without repository activity (state `disabled_inactivity`, which also makes `gh workflow run` report the workflow as not found).
