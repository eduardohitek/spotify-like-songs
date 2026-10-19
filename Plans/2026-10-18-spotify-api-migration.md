# Plan: Etapa 1 — migrar endpoints do Spotify e corrigir bugs

**Date:** 2026-10-18
**Status:** Approved

---

## Objective

Sair dos endpoints removidos na migração de fevereiro de 2026 da Web API do Spotify e corrigir os bugs
encontrados na revisão, mantendo o job funcionando no CI.

---

## Implementation Steps

1. Branch `fix/spotify-api-migration` a partir da `main`.
2. `POST /me/playlists` com `public: false` (booleano); remove o `userID` fixo.
3. `GET`/`POST /playlists/{id}/items`: leitura paginada (50 por página, campo `item`) e adição em lote via corpo JSON (até 100 URIs por chamada).
4. Mês calculado em `America/Sao_Paulo`, com `time/tzdata` embutido.
5. Processar o mês atual e o anterior; filtro por mês **e** ano; `/me/tracks` paginado até o início do mês anterior.
6. Criar playlist apenas quando houver músicas; não alterar a visibilidade das existentes.
7. `http.Client` compartilhado com timeout de 30s; fechar bodies por chamada; structs tipadas no lugar de asserções; log seguro para faixas sem artistas.
8. `go build`/`go vet`, run do workflow na branch, PR.

---

## Expected Outcomes

- Job independente dos endpoints removidos.
- Curtidas do fim do mês e de runs perdidos entram na playlist certa.
- Novas playlists criadas como privadas; sem playlists vazias.

---

## Risks & Considerations

- O run de validação altera playlists reais; a "Sep'26" provavelmente será criada com as curtidas de setembro.
- `/me/playlists` continua lendo só as 50 primeiras playlists (paginação fica para a etapa 2).
- Reorganização em pacotes, helper HTTP e testes ficam para as etapas 2 e 3.
