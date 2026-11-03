import type { DramaAssetForm, DramaStoryboardMention, DramaStoryboardSegment } from '../api/drama'

export interface StoryboardTextDocument { text: string; mentions: DramaStoryboardMention[] }
export const formLabel = (form: DramaAssetForm) => form.name && form.name !== '默认形象' ? `${form.assetName} · ${form.name}` : form.assetName
const count = (text: string) => Array.from(text).length

// 只标记当前片段真实绑定中可唯一识别的名称；同名多形象不猜测。
export function inferStoryboardMentions(text: string, forms: DramaAssetForm[], saved?: DramaStoryboardMention[]): DramaStoryboardMention[] {
  if (saved !== undefined) return saved
  const names = new Map<string, Set<string>>()
  for (const form of forms) for (const name of new Set([form.assetName, formLabel(form), `${form.assetName}-${form.name}`])) {
    if (count(name) < 2) continue
    const ids = names.get(name) ?? new Set<string>(); ids.add(form.id); names.set(name, ids)
  }
  const chars = Array.from(text), result: DramaStoryboardMention[] = []
  const candidates = [...names].filter(([, ids]) => ids.size === 1).sort((a, b) => count(b[0]) - count(a[0]))
  for (let i = 0; i < chars.length;) {
    const found = candidates.find(([name]) => chars.slice(i, i + count(name)).join('') === name)
    if (!found) { i++; continue }
    const end = i + count(found[0]); result.push({ start: i, end, formId: [...found[1]][0] }); i = end
  }
  return result
}
export function materializeStoryboardMentions(segments: DramaStoryboardSegment[], forms: DramaAssetForm[]) {
  return structuredClone(segments).map((s) => {
    const bound = forms.filter((f) => s.formIds.includes(f.id))
    s.sceneMentions = inferStoryboardMentions(s.scene, bound, s.sceneMentions)
    s.purposeMentions = inferStoryboardMentions(s.purpose, bound, s.purposeMentions)
    for (const shot of s.shots) {
      shot.descriptionMentions = inferStoryboardMentions(shot.description, bound, shot.descriptionMentions)
      shot.soundMentions = inferStoryboardMentions(shot.sound, bound, shot.soundMentions)
      for (const u of shot.utterances) {
        u.mentions = inferStoryboardMentions(u.text, bound, u.mentions)
        u.speakerMentions = inferStoryboardMentions(u.speaker, bound, u.speakerMentions)
      }
    }
    return s
  })
}
