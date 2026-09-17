// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { useEffect } from 'react'
import { create } from 'zustand'
import { APP } from './about'

// Atualização do servidor com a aba aberta.
//
// O index.html é servido com `no-store` e os assets têm hash no nome (`immutable`), então um
// reload já troca o bundle inteiro — o que falta é a aba saber que precisa recarregar. Toda
// resposta traz `X-Filezam-Version` (middleware securityHeaders); o `api()` em api/client.ts
// passa o valor por aqui e a comparação com a versão embutida no bundle dispara a faixa. Custo
// zero: nenhuma requisição existe só para isso, e a consulta de não lidas (30 s com a aba
// visível) faz até uma aba parada descobrir sozinha.

interface UpdateState {
  /** Versão nova vista no header; null enquanto o servidor for o mesmo desta aba. */
  serverVersion: string | null
  note: (v: string | null) => void
}

export const useUpdate = create<UpdateState>((set, get) => ({
  serverVersion: null,
  note: (v) => {
    if (!isOtherVersion(v) || get().serverVersion === v) return
    set({ serverVersion: v })
  },
}))

// isOtherVersion só afirma a diferença quando os dois lados dizem um número. 'dev' é o valor do
// binário sem `-X main.version` (make run) e o do bundle sem o `define` do Vite: em
// desenvolvimento o servidor é 'dev' e a interface tem o número do package.json, e sem esta
// guarda a faixa apareceria o tempo todo no `make dev`.
export function isOtherVersion(v: string | null | undefined): v is string {
  return !!v && v !== 'dev' && APP.version !== 'dev' && v !== APP.version
}

// ---- travas do recarregamento automático ----
//
// A faixa só recarrega sozinha quando nada se perde nisso. Cada tela que tem trabalho em
// andamento — envio na fila, editor aberto, diálogo na frente, recortar/copiar esperando colar —
// segura o reload enquanto estiver assim. É de propósito mais abrangente que o `beforeunload`,
// que só cobre perda de dados: fechar um editor sem alterações não perde nada, mas ninguém quer
// voltar para a aba e encontrá-lo fechado.
let holds = 0

/** Segura o recarregamento automático enquanto `active`. */
export function useReloadHold(active: boolean) {
  useEffect(() => {
    if (!active) return
    holds++
    return () => {
      holds--
    }
  }, [active])
}

export const safeToReload = () => holds === 0

/** Só para os testes: zera as travas deixadas por um componente desmontado na marra. */
export function resetReloadHolds() {
  holds = 0
}
