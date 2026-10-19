# Plan: Corrigir falha do CI por refresh token revogado

**Date:** 2026-10-18
**Status:** Approved

---

## Objective

O workflow "Spotify Automation" falha desde 16/05/2026 com `invalid_grant: Refresh token revoked`.
O token é gerado via PKCE (`cmd/auth`), mas renovado pelo `main.go` com Basic auth e o
`refresh_token` rotacionado é descartado. Alinhar os fluxos e endurecer o repositório.

---

## Implementation Steps

1. Criar branch `fix/spotify-refresh-token` a partir da `main`.
2. `cmd/auth/main.go`: versionar e trocar PKCE por Authorization Code com Basic auth.
3. `main.go`: corpo do token via `url.Values`; capturar `refresh_token` e só avisar (sem o valor) quando vier um novo.
4. Workflow: `actions/checkout@v4` e `actions/setup-go@v5`.
5. `git rm --cached .env.local`, criar `.env.example`, limpar `.gitignore` e adicionar `auth`, `spotify-cli` e `.serena/`.
6. Commit, push e abrir PR.

---

## Expected Outcomes

- Token gerado pelo `cmd/auth` é renovável pelo `main.go` sem rotação.
- Segredos locais não podem ser commitados por acidente.
- Fim dos avisos de Node 20 no Actions.

---

## Risks & Considerations

- Após o merge, o usuário precisa gerar um token novo (`go run ./cmd/auth`), atualizar o secret e disparar o workflow.
- Usar um token separado para rodar localmente, para não invalidar o do CI.
- `git rm --cached` remove o `.env.local` do repositório para quem clonar; o `.env.example` substitui como modelo.
