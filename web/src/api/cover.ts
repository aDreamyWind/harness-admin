export interface CoverHistoryItem {
  revision: string
  source: 'ai' | 'upload'
  url: string
  createdAt: string
  current: boolean
}
