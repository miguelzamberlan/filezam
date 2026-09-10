/** Recursively collect files from a drop event or an <input> selection. */

export interface PickedFile {
  file: File
  relPath: string // path relative to the destination, using '/'
}

type Entry = FileSystemEntry & {
  isFile: boolean
  isDirectory: boolean
}

function readAllEntries(reader: FileSystemDirectoryReader): Promise<Entry[]> {
  return new Promise((resolve, reject) => {
    const out: Entry[] = []
    const step = () =>
      reader.readEntries((batch) => {
        if (batch.length === 0) return resolve(out)
        out.push(...(batch as Entry[]))
        step() // readEntries returns at most ~100 entries per call
      }, reject)
    step()
  })
}

function entryFile(entry: FileSystemFileEntry): Promise<File> {
  return new Promise((resolve, reject) => entry.file(resolve, reject))
}

const yieldToMain = () => new Promise<void>((r) => setTimeout(r, 0))

async function walkEntry(entry: Entry, prefix: string, onFile: (f: PickedFile) => void, onDir: (rel: string) => void): Promise<void> {
  if (entry.isFile) {
    try {
      const file = await entryFile(entry as FileSystemFileEntry)
      onFile({ file, relPath: prefix + entry.name })
    } catch {
      /* unreadable entry */
    }
    return
  }
  if (entry.isDirectory) {
    const rel = prefix + entry.name
    onDir(rel)
    const entries = await readAllEntries((entry as FileSystemDirectoryEntry).createReader())
    let i = 0
    for (const child of entries) {
      await walkEntry(child, rel + '/', onFile, onDir)
      if (++i % 50 === 0) await yieldToMain()
    }
  }
}

/** Collect files from a DataTransfer (drag and drop). Emits directories so empty folders can be created. */
export async function collectFromDataTransfer(
  dt: DataTransfer,
  onFile: (f: PickedFile) => void,
  onDir: (rel: string) => void = () => {},
): Promise<void> {
  const items = Array.from(dt.items ?? [])
  const entries: Entry[] = []
  for (const it of items) {
    if (it.kind !== 'file') continue
    const e = (it.webkitGetAsEntry?.() ?? null) as Entry | null
    if (e) entries.push(e)
  }
  if (entries.length === 0) {
    for (const f of Array.from(dt.files ?? [])) onFile({ file: f, relPath: f.name })
    return
  }
  for (const e of entries) await walkEntry(e, '', onFile, onDir)
}

/** Collect files from an <input type=file> (with or without webkitdirectory). */
export function collectFromFileList(files: FileList | File[]): PickedFile[] {
  const out: PickedFile[] = []
  for (const f of Array.from(files)) {
    const rel = (f as File & { webkitRelativePath?: string }).webkitRelativePath || f.name
    out.push({ file: f, relPath: rel })
  }
  return out
}
