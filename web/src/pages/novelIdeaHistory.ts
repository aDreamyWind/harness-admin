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

export function matchingIdeaSuggestions(history: NovelIdeaHistoryEntry[], source: string): string[] {
  const current = comparableIdea(source)
  if (!current) return []
  const currentPairs = adjacentPairs(current)
  const candidates: { exact: boolean; savedAt: number; result: string; key: string }[] = []

  for (const entry of history) {
    const result = entry.result?.trim()
    if (!result || entry.instruction?.trim()) continue
    const original = comparableIdea(entry.source)
    const exact = original === current
    if (!exact && !similarIdeas(currentPairs, adjacentPairs(original))) continue
    const key = comparableIdea(result)
    if (!key || key === original) continue
    candidates.push({ exact, savedAt: entry.savedAt, result, key })
  }

  candidates.sort((first, second) => Number(second.exact) - Number(first.exact) || second.savedAt - first.savedAt)
  const seen = new Set<string>()
  const suggestions: string[] = []
  for (const candidate of candidates) {
    if (seen.has(candidate.key)) continue
    seen.add(candidate.key)
    suggestions.push(candidate.result)
  }
  return suggestions
}

const ideaExcerptLimit = 500

export function ideaSuggestionExcerpts(results: string[]): string[] {
  const seen = new Set<string>()
  const suggestions: string[] = []
  for (const result of results) {
    const excerpt = Array.from(result.trim()).slice(0, ideaExcerptLimit).join('').trimEnd()
    const key = comparableIdea(excerpt)
    if (!key || seen.has(key)) continue
    seen.add(key)
    suggestions.push(excerpt)
    if (suggestions.length === 3) break
  }
  return suggestions
}

export function findRepeatedIdeaSuggestion(results: string[], source: string, candidate: {
  brief: string; protagonistName?: string; systemName?: string
}): { suggestion: string; name?: string } | undefined {
  const original = comparableIdea(source)
  const generated = comparableIdea(candidate.brief)
  const opening = Array.from(generated).slice(0, ideaExcerptLimit)
  const names = [comparableIdea(candidate.protagonistName ?? ''), systemNameKey(candidate.systemName ?? '')]
    .filter(name => name && generated.includes(name) && !original.includes(name))

  for (const suggestion of results) {
    const key = comparableIdea(suggestion)
    if (!key) continue
    const name = names.find(name => key.includes(name))
    if (name) return { suggestion, name }
    if (!original.includes(key) && (generated === key || similarIdeaOpening(opening, key))) return { suggestion }
  }
}

function systemNameKey(value: string): string {
  let key = comparableIdea(value)
  for (const suffix of ['辅助器', '模拟器', '系统', '助手', '面板', '外挂']) {
    if (!key.endsWith(suffix)) continue
    const stem = key.slice(0, -suffix.length)
    if (Array.from(stem).length >= 2) key = stem
  }
  return key
}

function similarIdeaOpening(opening: string[], previous: string): boolean {
  const old = Array.from(previous)
  const length = Math.min(opening.length, old.length, ideaExcerptLimit)
  if (length < 40) return false

  const grams = new Map<string, number>()
  for (let index = 0; index + 4 <= length; index++) {
    const key = old.slice(index, index + 4).join('')
    grams.set(key, (grams.get(key) ?? 0) + 1)
  }
  let shared = 0
  for (let index = 0; index + 4 <= length; index++) {
    const key = opening.slice(index, index + 4).join('')
    const remaining = grams.get(key) ?? 0
    if (!remaining) continue
    grams.set(key, remaining - 1)
    shared++
  }
  return shared * 5 >= (length - 3) * 4
}

function comparableIdea(text: string): string {
  return text.normalize('NFKC').toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '')
}

function adjacentPairs(text: string): Set<string> {
  const characters = Array.from(text)
  const pairs = new Set<string>()
  for (let index = 1; index < characters.length; index++) pairs.add(characters[index - 1] + characters[index])
  return pairs
}

function similarIdeas(current: Set<string>, previous: Set<string>): boolean {
  let overlap = 0
  for (const pair of previous) if (current.has(pair)) overlap++
  // 短词重合不足以说明灵感相近。较长灵感同时按共有二元组比例筛选。
  return overlap >= 6 && (2 * overlap) / (current.size + previous.size) >= 0.65
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
