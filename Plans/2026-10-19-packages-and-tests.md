# Plan: Etapas 2 e 3 — reorganizar em pacotes, testes e CI de PR

**Date:** 2026-10-19
**Status:** Approved

---

## Objective

Reorganizar o job em `cmd/` + `internal/`, com client do Spotify próprio (só stdlib), logs com
`log/slog`, cobertura de testes e um workflow de CI para PRs, sem mudar o comportamento entregue na PR #5.

---

## Implementation Steps

1. Branch `refactor/packages-and-tests` empilhada sobre `fix/spotify-api-migration` (PR #5).
2. `internal/spotify`: `Client` com base URL configurável, `http.Client` com timeout, helper HTTP único
   (status, erro do Spotify, fechamento do body), paginação via `next` em todas as listagens
   (inclusive `/me/playlists`), `Authenticate` e `ExchangeCode`.
3. `internal/playlist`: regras puras (mês, nome, filtro, faixas novas) e `Updater` com o fluxo por mês;
   constantes de fuso e descrição.
4. `cmd/monthly-playlist`: `run(ctx) error` com timeout geral, `slog.TextHandler`, `.env.local` opcional sem panic.
5. `cmd/auth`: reaproveita `ExchangeCode`, abre navegador em macOS/Linux/Windows, canais com buffer.
6. Testes: tabela em `internal/playlist`, `httptest` no `spotify.Client`, fluxo completo contra Spotify falso.
7. `.github/workflows/ci.yaml`: `go vet`, `go test -race`, `gofmt`, em PRs e push na `main`.
8. Workflow principal roda `go run ./cmd/monthly-playlist`; atualizar `CLAUDE.md` e `.gitignore`.
9. Commits separados, run do workflow na branch, PR apontando para `fix/spotify-api-migration`.

---

## Expected Outcomes

- Mesmo comportamento da PR #5, com código testável e testado.
- Erros propagados até um único ponto de saída; logs estruturados.
- PRs validadas automaticamente.

---

## Risks & Considerations

- PR empilhada: mudanças na #5 exigem rebase.
- A #4 também altera o workflow principal; quem entrar por último pode precisar de rebase simples.
- O run de validação usa a conta real do Spotify.
