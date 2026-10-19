# Plan: Manter o workflow agendado ativo

**Date:** 2026-10-18
**Status:** Approved

---

## Objective

Evitar que o GitHub desative o workflow "Spotify Automation" (`disabled_inactivity`) após 60 dias sem atividade no repositório.

---

## Implementation Steps

1. Criar branch `ci/keepalive-workflow` a partir da `main`.
2. Adicionar job `keepalive` separado no mesmo workflow, com `permissions: actions: write`, que chama `PUT /repos/{repo}/actions/workflows/spotify-automation.yaml/enable` via `gh api`.
3. Commit, push e abrir PR.

---

## Expected Outcomes

- O agendamento diário permanece ativo indefinidamente, sem commits artificiais nem actions de terceiros.
- A permissão extra fica isolada do job que usa os secrets do Spotify.

---

## Risks & Considerations

- Depende de o GitHub continuar tratando a chamada de enable como atividade (mesmo método da keepalive-workflow v2).
- Mexe no mesmo arquivo da PR #1, em linhas diferentes; não deve haver conflito.
