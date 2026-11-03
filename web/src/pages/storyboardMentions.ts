import type { DramaAssetForm, DramaStoryboardMention, DramaStoryboardSegment } from '../api/drama'

export interface StoryboardTextDocument { text: string; mentions: DramaStoryboardMention[] }
export const formLabel = (form: DramaAssetForm) => form.name && form.name !== '默认形象' ? `${form.assetName} · ${form.name}` : form.assetName
const count = (text: string) => Array.from(text).length

// 场景口语简称去掉末尾的「子」，例如「小院子」在画面中写成「小院」。
export function storyboardSceneAlias(form: DramaAssetForm) {
  return form.kind === 'scene' && count(form.assetName) >= 3 && form.assetName.endsWith('子') ? form.assetName.slice(0, -1) : ''
}

export function inferStoryboardMentions(text: string, forms: DramaAssetForm[], saved?: DramaStoryboardMention[], sceneForms: DramaAssetForm[] = []): DramaStoryboardMention[] {
  if (saved !== undefined && !sceneForms.length) return saved
  const names = new Map<string, Set<string>>()
  function add(name: string, id: string) {
    if (count(name) < 2) return
    const ids = names.get(name) ?? new Set<string>()
    ids.add(id)
    names.set(name, ids)
  }
  if (saved === undefined) {
    for (const form of forms) for (const name of [form.assetName, formLabel(form), `${form.assetName}-${form.name}`]) add(name, form.id)
  }
  for (const form of sceneForms) {
    const alias = storyboardSceneAlias(form)
    if (!alias) continue
    add(alias, form.id)
    add(form.assetName, form.id)
    // 本集存在同名资产或多个形象时，保留人工选择。
    for (const other of [...forms, ...sceneForms]) {
      if (other.assetName === alias) add(alias, other.id)
    }
  }
  const chars = Array.from(text), result = [...(saved ?? [])]
  const candidates = [...names].filter(([, ids]) => ids.size === 1).map(([name, ids]) => ({ name, size: count(name), formId: [...ids][0] })).sort((a, b) => b.size - a.size)
  for (let i = 0; i < chars.length;) {
    const found = candidates.find(({ name, size }) => chars.slice(i, i + size).join('') === name && !saved?.some((ref) => ref.start < i + size && ref.end > i))
    if (!found) { i++; continue }
    const end = i + found.size
    result.push({ start: i, end, formId: found.formId })
    i = end
  }
  return result.sort((a, b) => a.start - b.start)
}
export function materializeStoryboardMentions(segments: DramaStoryboardSegment[], forms: DramaAssetForm[], sceneForms: DramaAssetForm[] = []) {
  return structuredClone(segments).map((s) => {
    const bound = forms.filter((f) => s.formIds.includes(f.id))
    s.sceneMentions = inferStoryboardMentions(s.scene, bound, s.sceneMentions)
    s.purposeMentions = inferStoryboardMentions(s.purpose, bound, s.purposeMentions)
    for (const shot of s.shots) {
      shot.descriptionMentions = inferStoryboardMentions(shot.description, bound, shot.descriptionMentions, sceneForms)
      shot.soundMentions = inferStoryboardMentions(shot.sound, bound, shot.soundMentions)
      for (const u of shot.utterances) {
        u.mentions = inferStoryboardMentions(u.text, bound, u.mentions)
        u.speakerMentions = inferStoryboardMentions(u.speaker, bound, u.speakerMentions)
      }
    }
    s.formIds = [...new Set([...s.formIds, ...s.shots.flatMap((shot) => (shot.descriptionMentions ?? []).map((mention) => mention.formId))])]
    return s
  })
}
