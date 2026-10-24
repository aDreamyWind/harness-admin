import { ApiError, csrfHeaders, request } from './client'

export interface SkillAsset {
  id: string
  name: string
  description: string
  status: 'draft' | 'published' | 'disabled'
  publishedVersionId: string | null
  latestVersionNo: number
  departmentIds: string[]
  userIds: string[]
  createdAt: string
  updatedAt: string
}

export interface SkillFile {
  path: string
  mimeType: string
  digest: string
  size: number
}

export interface SkillVersion {
  id: string
  skillId: string
  versionNo: number
  frontmatter: Record<string, unknown>
  manifestDigest: string
  sourceType: 'upload' | 'github'
  sourceRef: string
  sourceRevision: string
  sourcePath: string
  files: SkillFile[]
  createdAt: string
}

export const listSkills = () => request<SkillAsset[]>('/api/v1/skills')
export const listSkillVersions = (id: string) => request<SkillVersion[]>(`/api/v1/skills/${id}/versions`)

export function importSkill(file: File) {
  const body = new FormData()
  body.append('file', file)
  return request<SkillAsset>('/api/v1/skills/import', { method: 'POST', headers: csrfHeaders(), body })
}

export interface GitHubSkillImportInput {
  repositoryUrl: string
  ref: string
  skillPath: string
}

export function importGitHubSkill(input: GitHubSkillImportInput) {
  return request<{ asset: SkillAsset; revision: string }>('/api/v1/skills/import/github', {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(input),
  })
}

export function publishSkill(id: string, status: 'published' | 'disabled', versionId?: string) {
  return request<SkillAsset>(`/api/v1/skills/${id}/publish`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ status, versionId: versionId ?? '' }),
  })
}

export function replaceSkillGrants(id: string, departmentIds: string[], userIds: string[]) {
  return request<SkillAsset>(`/api/v1/skills/${id}/grants`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ departmentIds, userIds }),
  })
}

export async function readSkillVersionFile(id: string, versionId: string, path: string): Promise<string> {
  const response = await fetch(`/api/v1/skills/${id}/versions/${versionId}/files/${path.split('/').map(encodeURIComponent).join('/')}`, {
    credentials: 'same-origin', headers: { Accept: 'text/plain' },
  })
  if (!response.ok) throw new ApiError('读取 Skill 文件失败', response.status, 'skill_file_error')
  return response.text()
}
