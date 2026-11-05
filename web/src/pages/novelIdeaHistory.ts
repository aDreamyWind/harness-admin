export type NovelIdeaHistoryEntry = {
  id: string
  savedAt: number
  source: string
  result?: string
  modelName?: string
  instruction?: string
}

export function novelIdeaHistoryPrefix(scope: string): string {
  return `harness-novel-idea-history:v1:${encodeURIComponent(scope)}:`
}

export function listNovelIdeaHistory(scope: string): NovelIdeaHistoryEntry[] {
  const entries: NovelIdeaHistoryEntry[] = []
  if (!scope) return entries

  try {
    const storage = window.localStorage
    const prefix = novelIdeaHistoryPrefix(scope)
    for (const key of Object.keys(storage)) {
      if (!key.startsWith(prefix)) continue
      const entry = parseEntry(storage.getItem(key), key.slice(prefix.length))
      if (entry) entries.push(entry)
    }
  } catch {
    // 浏览器可能禁用本地存储，已读到的记录仍可查看。
  }

  return entries.sort((first, second) => second.savedAt - first.savedAt || second.id.localeCompare(first.id))
}

export function persistNovelIdeaHistoryEntry(scope: string, entry: NovelIdeaHistoryEntry): boolean {
  if (!scope) return false

  try {
    window.localStorage.setItem(novelIdeaHistoryPrefix(scope) + entry.id, JSON.stringify(entry))
    return true
  } catch {
    return false
  }
}

function parseEntry(raw: string | null, id: string): NovelIdeaHistoryEntry | undefined {
  if (!raw || !id) return undefined

  try {
    const entry = JSON.parse(raw) as Partial<NovelIdeaHistoryEntry> | null
    if (!entry || entry.id !== id || typeof entry.savedAt !== 'number' || !Number.isFinite(entry.savedAt)
      || typeof entry.source !== 'string' || !entry.source.trim()
      || (entry.result !== undefined && typeof entry.result !== 'string')
      || (entry.modelName !== undefined && typeof entry.modelName !== 'string')
      || (entry.instruction !== undefined && typeof entry.instruction !== 'string')) return undefined
    return { ...entry, id, savedAt: entry.savedAt, source: entry.source }
  } catch {
    // 忽略单条损坏记录，继续读取其余历史。
    return undefined
  }
}
