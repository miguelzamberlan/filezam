// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { create } from 'zustand'
import type { Job } from '../api/types'

interface JobsState {
  jobs: Job[]
  track: (j: Job) => void
  update: (j: Job) => void
  dismiss: (id: string) => void
}

export const useJobs = create<JobsState>((set) => ({
  jobs: [],
  track: (j) => set((s) => (s.jobs.some((x) => x.id === j.id) ? s : { jobs: [...s.jobs, j] })),
  update: (j) => set((s) => ({ jobs: s.jobs.map((x) => (x.id === j.id ? j : x)) })),
  dismiss: (id) => set((s) => ({ jobs: s.jobs.filter((x) => x.id !== id) })),
}))
