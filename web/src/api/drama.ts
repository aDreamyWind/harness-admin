import { csrfHeaders, request } from './client'

export interface DramaModel { code: string; name: string; modelType: string }
export interface DramaSettings {
  textModelCode: string
  imageModelCode: string
  videoModelCode: string
  updatedAt?: string
}
export interface DramaBatchReference { id: string; name: string; number: number }
export interface DramaAsset { id?: string; name: string; description: string; appearanceDescription?: string; voiceDescription?: string; sourceBatch?: DramaBatchReference | null; usedInBatches?: DramaBatchReference[] }
export interface DramaEpisode { title: string; synopsis: string }
export interface DramaOverview {
  styleId?: string
  videoStyle?: string
  aspectRatio?: string
  protagonist?: string
  genre?: string
  synopsis?: string
  background?: string
  setting?: string
}
export interface DramaProject {
  id: string
  title: string
  sourceName: string
  sourceLength: number
  sourceMode: 'upload' | 'generate'
  styleId?: string
  style: string
  aspectRatio: '9:16' | '16:9'
  modelCode: string
  status: 'queued' | 'running' | 'ready' | 'failed'
  phase: string
  error?: string
  overview: DramaOverview
  characters: DramaAsset[]
  scenes: DramaAsset[]
  props: DramaAsset[]
  episodes: DramaEpisode[]
  createdAt: string
  updatedAt: string
}
export interface CreateDramaInput {
  title: string
  sourceName: string
  sourceText: string
  sourceMode: 'upload' | 'generate'
  styleId?: string
  style?: string
  aspectRatio: '9:16' | '16:9'
}

export const listDramaModels = () => request<DramaModel[]>('/api/v1/ai-drama/models')
export const getDramaSettings = () => request<DramaSettings>('/api/v1/ai-drama/settings')
export const saveDramaSettings = (input: DramaSettings) => request<DramaSettings>('/api/v1/ai-drama/settings', {
  method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
})
export const listDramaProjects = () => request<DramaProject[]>('/api/v1/ai-drama/projects')
export const getDramaProject = (id: string) => request<DramaProject>(`/api/v1/ai-drama/projects/${id}`)
export const getDramaSource = (id: string) => request<{ sourceText: string }>(`/api/v1/ai-drama/projects/${id}/source`)
export const retryDramaProject = (id: string) => request<DramaProject>(`/api/v1/ai-drama/projects/${id}/retry`, {
  method: 'POST', headers: csrfHeaders(),
})
export const reanalyzeDramaOverview = (id: string) => request<DramaProject>(`/api/v1/ai-drama/projects/${id}/reanalyze-overview`, {
  method: 'POST', headers: csrfHeaders(),
})
export const createDramaProject = (input: CreateDramaInput) => request<DramaProject>('/api/v1/ai-drama/projects', {
  method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
})
export const updateDramaOverview = (id: string, overview: DramaOverview) => request<DramaProject>(`/api/v1/ai-drama/projects/${id}/overview`, {
  method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(overview),
})

export interface DramaSourceRange {
  startOffset: number
  endOffset: number
  totalLength: number
  text: string
}
export type DramaBatchMode = 'beginning' | 'previous' | 'manual'
export interface DramaBatchSelection {
  mode: DramaBatchMode
  previousBatchId?: string
  startOffset?: number
  endOffset?: number
}
export interface DramaBatchSuggestion { startOffset: number; endOffset: number; boundary: 'episode' | 'chapter' | 'paragraph' | 'limit' | 'end' }
export interface DramaBatch {
  name: string
  nameManuallyEdited: boolean
  id: string
  projectId: string
  number: number
  startOffset: number
  endOffset: number
  selectionMode: DramaBatchMode
  previousBatchId?: string
  boundary: DramaBatchSuggestion['boundary'] | 'manual'
  status: 'pending' | 'running' | 'failed' | 'stale' | 'ready'
  errorMessage: string
  analysisRevision: number
  revision: number
  createdAt: string
  updatedAt: string
}
export interface DramaBatchRangeChange { startOffset: number; endOffset: number; revision: number }

export const getDramaSourceRange = (id: string, start: number, limit = 12000) =>
  request<DramaSourceRange>(`/api/v1/ai-drama/projects/${id}/source/range?start=${start}&limit=${limit}`)
export const listDramaBatches = (id: string) => request<DramaBatch[]>(`/api/v1/ai-drama/projects/${id}/batches`)
export const suggestDramaBatch = (id: string, input: Omit<DramaBatchSelection, 'endOffset'>) => {
  const query = new URLSearchParams({ mode: input.mode })
  if (input.previousBatchId) query.set('previousBatchId', input.previousBatchId)
  if (input.startOffset !== undefined) query.set('startOffset', String(input.startOffset))
  return request<DramaBatchSuggestion>(`/api/v1/ai-drama/projects/${id}/batches/suggest?${query}`)
}
export const createDramaBatch = (id: string, input: DramaBatchSelection) => request<DramaBatch>(`/api/v1/ai-drama/projects/${id}/batches`, {
  method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
})
export const renameDramaBatch = (id: string, batchId: string, name: string) => request<DramaBatch>(`/api/v1/ai-drama/projects/${id}/batches/${batchId}/name`, {
  method: 'PATCH', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ name }),
})
export const updateDramaBatchRange = (id: string, batchId: string, input: DramaBatchRangeChange) => request<DramaBatch>(`/api/v1/ai-drama/projects/${id}/batches/${batchId}/range`, {
  method: 'PATCH', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
})

export type DramaAssetKind = 'character' | 'scene' | 'prop'
export interface DramaCatalogAsset extends DramaAsset { id: string; kind: DramaAssetKind; name: string; description: string; appearanceDescription: string; voiceDescription: string }
// id 为本集关联记录标识，assetId 为项目资产目录中的真实主键。
export interface DramaEpisodeAsset extends DramaCatalogAsset { assetId: string; stateLabel: string }
export interface DramaScriptEpisode {
  id: string; number: number; title: string; synopsis: string; scriptText: string
  characters: DramaEpisodeAsset[]; scenes: DramaEpisodeAsset[]; props: DramaEpisodeAsset[]
}
export interface DramaBatchResult {
  batch: DramaBatch
  assets: { characters: DramaCatalogAsset[]; scenes: DramaCatalogAsset[]; props: DramaCatalogAsset[] }
  episodes: DramaScriptEpisode[]
}
export interface DramaBatchStep {
  stage: 'art' | 'story' | 'evidence' | 'context' | 'plan' | 'name' | 'director' | 'review' | 'repair'
  chunkNumber: number
  chunkTotal: number
  status: 'running' | 'completed' | 'failed'
  errorMessage?: string
  durationMs: number
  startedAt: string
  updatedAt: string
}
export interface DramaSourceReference { id?: string; quote: string; startOffset: number; endOffset: number }
export interface DramaSourceFact { sourceIds?: string[]; references?: DramaSourceReference[]; id: string; unitId: number; claim: string; quote: string; startOffset: number; endOffset: number }
export interface DramaGroundedContext { startOffset: number; endOffset: number; precedingStart: number; precedingEnd: number; summary: string; background: string; setting: string; continuity: string }
export interface DramaReviewIssue { id: string; episodeNumber: number; category: 'fact' | 'continuity' | 'causality' | 'style'; severity: 'blocking' | 'suggestion'; message: string; sourceQuote: string; scriptQuote: string; sourceStart: number; sourceEnd: number; status: 'open' | 'resolved'; createdAt: string; updatedAt: string }
export interface DramaEpisodePlan { title: string; synopsis: string; hook: string; startUnit: number; endUnit: number; factIds?: string[]; sources?: DramaSourceFact[] }
export interface DramaBatchEvent { id: number; stage: string; stepNumber: number; kind: string; message: string; createdAt: string }
export interface DramaBatchProgress {
 repairs?: DramaRepairTicket[]
 context?: DramaGroundedContext
 reviewIssues?: DramaReviewIssue[]
  plans: DramaEpisodePlan[]
  steps: DramaBatchStep[]
  assets: { characters: DramaCatalogAsset[]; scenes: DramaCatalogAsset[]; props: DramaCatalogAsset[] }
  episodes: Array<{ title: string; synopsis: string; scriptText: string }>
}
export const getDramaBatchProgress = (projectId: string, batchId: string) =>
  request<DramaBatchProgress>(`/api/v1/ai-drama/projects/${projectId}/batches/${batchId}/progress`)
export const resumeDramaBatch = (projectId: string, batchId: string) => request<DramaBatch>(`/api/v1/ai-drama/projects/${projectId}/batches/${batchId}/resume`, { method: 'POST', headers: csrfHeaders() })
export const analyzeDramaBatch = (projectId: string, batchId: string) => request<DramaBatch>(`/api/v1/ai-drama/projects/${projectId}/batches/${batchId}/analyze`, { method: 'POST', headers: csrfHeaders() })
export const getDramaBatchResult = (projectId: string, batchId: string) => request<DramaBatchResult>(`/api/v1/ai-drama/projects/${projectId}/batches/${batchId}/result`)
export const listDramaAssets = (projectId: string, kind: DramaAssetKind, q = '') => request<DramaCatalogAsset[]>(`/api/v1/ai-drama/projects/${projectId}/assets?kind=${kind}&q=${encodeURIComponent(q)}`)
export const addDramaEpisodeAsset = (projectId: string, episodeId: string, assetId: string, stateLabel = '') => request<DramaEpisodeAsset>(`/api/v1/ai-drama/projects/${projectId}/episodes/${episodeId}/assets`, { method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ assetId, stateLabel }) })
export const removeDramaEpisodeAsset = (projectId: string, episodeId: string, linkId: string) => request<{ removed: boolean }>(`/api/v1/ai-drama/projects/${projectId}/episodes/${episodeId}/assets/${linkId}`, { method: 'DELETE', headers: csrfHeaders() })

export interface DramaVideoEpisode {
  scriptEpisodeId: string
  projectId: string
  number: number
  batchEpisodeNumber: number
  batch: DramaBatchReference
  title: string
  synopsis: string
  storyboardStatus: 'pending' | 'queued' | 'running' | 'failed' | 'ready' | 'confirmed' | 'partial' | 'stale'
  characterCount: number
  sceneCount: number
  propCount: number
  scriptNeedsReview: boolean
  scriptUpdatedAt: string
}
export interface DramaVideoEpisodeWorkspace {
  episode: DramaVideoEpisode
  scriptText: string
  characters: DramaEpisodeAsset[]
  scenes: DramaEpisodeAsset[]
  props: DramaEpisodeAsset[]
}
export const listDramaVideoEpisodes = (projectId: string) => request<DramaVideoEpisode[]>(`/api/v1/ai-drama/projects/${projectId}/video-episodes`)
export const getDramaVideoEpisodeWorkspace = (projectId: string, episodeId: string) => request<DramaVideoEpisodeWorkspace>(`/api/v1/ai-drama/projects/${projectId}/video-episodes/${episodeId}`)

export interface DramaAssetForm {
  id: string; assetId: string; assetName: string; kind: DramaAssetKind; parentId?: string
  name: string; description: string; gender: 'unspecified' | 'male' | 'female' | 'other'
  styleId?: string; style: string; aspectRatio: '9:16' | '16:9' | '1:1'; x: number; y: number; revision: number
  imageUrl?: string; imageStatus: 'idle' | 'queued' | 'running' | 'succeeded' | 'failed'; imageError?: string
}
export interface DramaCanvas { forms: DramaAssetForm[]; x: number; y: number; zoom: number; revision: number }
export interface DramaCanvasLayout { x: number; y: number; zoom: number; revision: number; positions: { id: string; x: number; y: number }[] }
export interface DramaImageJob { id: string; formId: string; projectId: string; status: string; modelCode: string }
const visualBase = (id: string) => `/api/v1/ai-drama/projects/${id}`
export const getDramaCanvas = (id: string) => request<DramaCanvas>(`${visualBase(id)}/canvas`)
export const saveDramaCanvasLayout = (id: string, input: DramaCanvasLayout) => request<{ revision: number }>(`${visualBase(id)}/canvas/layout`, {
  method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
})
export const editDramaForm = (id: string, form: DramaAssetForm) => request<{ saved: boolean }>(`${visualBase(id)}/forms/${form.id}`, {
  method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ revision: form.revision, name: form.name, description: form.description, gender: form.gender, ...(form.styleId ? { styleId: form.styleId } : { style: form.style }), aspectRatio: form.aspectRatio }),
})
export const deriveDramaForm = (id: string, formId: string) => request<{ id: string }>(`${visualBase(id)}/forms/${formId}/derive`, { method: 'POST', headers: csrfHeaders() })
export const generateDramaImage = (id: string, formId: string, prompt = '') => request<DramaImageJob>(`${visualBase(id)}/forms/${formId}/images`, {
  method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ prompt }),
})

export interface DramaStyleOption { id: string; name: string; category: string; previewUrl: string; previewX: number; previewY: number }
export const listDramaStyles = () => request<DramaStyleOption[]>('/api/v1/ai-drama/styles')

export interface DramaStoryboardMention { start: number; end: number; formId: string }
export interface DramaStoryboardUtterance { mentions?: DramaStoryboardMention[]; speakerMentions?: DramaStoryboardMention[]; kind: 'dialogue' | 'os' | 'vo'; speaker: string; text: string; sourceLine: number; sourceText?: string; delivery?: 'spoken' | 'visual'; adaptationReason?: string }
export interface DramaStoryboardShot { descriptionMentions?: DramaStoryboardMention[]; soundMentions?: DramaStoryboardMention[]; id: string; number: number; description: string; shotSize: string; camera: string; sound: string; duration: number; sourceLines: number[]; utterances: DramaStoryboardUtterance[] }
export interface DramaStoryboardSegment { version?: number; lineageId?: string; versions?: { version: number; runId: string; segmentId: string }[]; videoReferences?: { formId: string; imageId: string; name: string }[]; sceneMentions?: DramaStoryboardMention[]; purposeMentions?: DramaStoryboardMention[]; id: string; number: number; title: string; scene: string; purpose: string; startLine: number; endLine: number; startState: string; endState: string; status: string; notes: string; formIds: string[]; bindings: {formId: string; imageId: string}[]; shots: DramaStoryboardShot[] }
export interface DramaStoryboardRun { pacingTarget?: number; repairs?: DramaRepairTicket[]; id: string; episodeId: string; projectId: string; version: number; revision: number; status: string; phase: string; message: string; error: string; scriptText: string; modelCode: string; styleId: string; aspectRatio: string; createdAt: string; updatedAt: string; stale: boolean; segments: DramaStoryboardSegment[]; events: {id: number; message: string; createdAt: string}[] }
export interface DramaStoryboardPage { catalogForms?: DramaAssetForm[]; runs: DramaStoryboardRun[]; current: DramaStoryboardRun | null; forms: DramaAssetForm[] }
const storyboardBase = (project: string, episode: string) => `${visualBase(project)}/video-episodes/${episode}/storyboards`
export const getDramaStoryboards = (project: string, episode: string, run = '') => request<DramaStoryboardPage>(`${storyboardBase(project, episode)}${run ? `?run=${encodeURIComponent(run)}` : ''}`)
export const startDramaStoryboard = (project: string, episode: string, resume = false) => request<DramaStoryboardRun>(storyboardBase(project, episode), { method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ resume }) })
export const saveDramaStoryboard = (project: string, episode: string, run: DramaStoryboardRun, segments: DramaStoryboardSegment[]) => request<DramaStoryboardRun>(`${storyboardBase(project, episode)}/${run.id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ revision: run.revision, segments }) })
export const optimizeDramaStoryboardPacing = (project: string, episode: string, run: DramaStoryboardRun, segmentId: string) => request<DramaStoryboardRun>(`${storyboardBase(project, episode)}/${run.id}/optimize-pacing`, { method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ revision: run.revision, segmentId }) })

export interface DramaRepairTicket { id: string; kind: 'batch' | 'storyboard'; item: number; reporter: string; problem: string; node: string; targetItem: number; reason: string; instructions: string; returnTo: string; status: 'assigned' | 'resolved' | 'feedback' | 'blocked' | 'declined'; beforeShotCount?: number; afterShotCount?: number; beforeDuration?: number; afterDuration?: number; outcome: string; createdAt: string }

export interface DramaSegmentVideo {
  id: string
  segmentId: string
  segmentVersion: number
  revision: number
  modelCode: string
  duration: number
  imageIds: string[]
  status: 'running' | 'failed' | 'succeeded'
  error?: string
  createdAt: string
}

const segmentVideoBase = (project: string, episode: string, run: string, segment: string) =>
  `${storyboardBase(project, episode)}/${run}/segments/${segment}/videos`
export const listDramaSegmentVideos = (project: string, episode: string, run: string, segment: string) =>
  request<DramaSegmentVideo[]>(segmentVideoBase(project, episode, run, segment))
export const startDramaSegmentVideo = (project: string, episode: string, run: string, segment: string, revision: number) =>
  request<DramaSegmentVideo>(segmentVideoBase(project, episode, run, segment), {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ revision }),
  })
export const dramaSegmentVideoURL = (project: string, episode: string, run: string, segment: string, video: string) =>
  `${segmentVideoBase(project, episode, run, segment)}/${video}/content`

export interface DramaSegmentVideoCall {
  operation: 'video' | 'video_task_query'
  result: 'success' | 'failed'
  httpStatus: number
  model: string
  code?: string
  message?: string
  createdAt: string
}

export const listDramaSegmentVideoCalls = (project: string, episode: string, run: string, segment: string, video: string) =>
  request<DramaSegmentVideoCall[]>(`${segmentVideoBase(project, episode, run, segment)}/${video}/calls`)
