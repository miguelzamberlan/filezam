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
