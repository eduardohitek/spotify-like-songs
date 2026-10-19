# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go job that copies Spotify liked songs into private monthly playlists named like `Oct'26` (`Jan'YY` format). Each run updates the current **and** previous month, so songs liked after the previous month's last run are not lost. It runs daily in GitHub Actions (`.github/workflows/spotify-automation.yaml`). The GitHub repo is `eduardohitek/spotify-like-songs`, although the Go module is `github.com/eduardohitek/spotify-cli`.

## Commands

```bash
go run ./cmd/monthly-playlist   # run the job (reads .env.local if present, else env vars)
go run ./cmd/auth               # one-off OAuth flow that prints a new SPOTIFY_REFRESH_TOKEN
go test -race ./...             # all tests; no network, Spotify is faked with httptest
go test ./internal/playlist -run TestUpdaterRun
go vet ./... && gofmt -l .
```

`.github/workflows/ci.yaml` runs gofmt, vet and tests on PRs and pushes to `main`.

Operating the scheduled job:
```bash
gh workflow run "Spotify Automation" [--ref <branch>]   # a --ref run touches the real playlists
gh run watch
gh secret set SPOTIFY_REFRESH_TOKEN
```

## Configuration

`SPOTIFY_CLIENT_ID`, `SPOTIFY_CLIENT_SECRET` and `SPOTIFY_REFRESH_TOKEN` come from `.env.local` locally (untracked; `.env.example` is the template) and from repository secrets in CI. The time zone (`America/Sao_Paulo`) and playlist description are constants in `internal/playlist`.

## Architecture

- `internal/spotify` — stdlib-only Web API client. One request helper (`doJSON`/`send`) handles auth headers, non-2xx → `*APIError` (parsing both the Web API and the accounts-service error formats) and body closing; every listing follows `next` for pagination. `APIURL`/`TokenURL` are fields so tests point them at `httptest` servers.
- `internal/playlist` — pure month rules (`MonthStart`, `Name`, `TracksForMonth`, `NewTracks`) and `Updater`, which runs the per-month flow against the small `Client` interface. A playlist is created (private) only when the month has songs; existing playlists are never modified except to append missing tracks.
- `cmd/monthly-playlist` — wiring only: env/config, `slog` text logger, `run(ctx) error` with an overall timeout.
- `cmd/auth` — local helper for the Authorization Code flow on `http://127.0.0.1:8080/callback` (must be a registered redirect URI in the Spotify app); reuses `spotify.Client.ExchangeCode`.

`internal/playlist/updater_test.go` contains an in-memory fake Spotify (`fakeSpotify`) with small pages; extend it when the flow uses a new endpoint.

## Things to know before changing code

- **Spotify API version.** The code targets the post-February-2026 Web API: `POST /me/playlists` and `/playlists/{id}/items` (entries under `item`, max 50 per page, max 100 URIs per add). The older `/users/{id}/playlists` and `/playlists/{id}/tracks` are removed for Development Mode apps; do not reintroduce them.
- **Token flows must match.** `cmd/auth` obtains the token with the confidential Authorization Code flow (Basic auth) and the job refreshes it the same way. Do not switch to PKCE: PKCE refresh tokens rotate, and the CI secret ends up revoked (`invalid_grant: Refresh token revoked`).
- **One refresh token per environment.** Use a separate token locally from the one in the `SPOTIFY_REFRESH_TOKEN` secret. If the job warns that Spotify rotated the token, the secret needs regenerating; the new value is deliberately never logged because CI would not mask it.
- Liked songs are compared by instant against month boundaries in `America/Sao_Paulo`; the CI runs around midnight UTC, i.e. late evening in São Paulo.
- The workflow has a `keepalive` job that re-enables itself via the API, because GitHub disables scheduled workflows after 60 days without repository activity (state `disabled_inactivity`, which also makes `gh workflow run` report the workflow as not found).
