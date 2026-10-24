import { ArrowLeftOutlined, FileImageOutlined, FileTextOutlined, FolderOpenOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Empty, Segmented, Select, Skeleton, Space, Tabs, Tag, Tree, Typography } from 'antd'
import { useEffect, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'

import { listSkills, listSkillVersions, readSkillVersionBlob, type SkillFile } from '../api/skills'

type FileNode = { key: string; title: string; icon?: React.ReactNode; children?: FileNode[]; isLeaf?: boolean }
type PreviewMode = '预览' | '源码'

const errorText = (error: unknown) => error instanceof Error ? error.message : '读取失败，请重试'
const rasterTypes: Record<string, string> = { png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp' }
const textExtensions = new Set(['md', 'mdx', 'txt', 'json', 'jsonl', 'yaml', 'yml', 'toml', 'xml', 'csv', 'tsv',
  'js', 'jsx', 'ts', 'tsx', 'py', 'go', 'sh', 'bash', 'css', 'scss', 'html', 'sql', 'env', 'ini', 'cfg', 'gitignore', 'license'])

function fileKind(file: SkillFile): 'markdown' | 'image' | 'text' | 'unsupported' {
  const extension = file.path.split('.').pop()?.toLowerCase() ?? ''
  if ((extension === 'md' || extension === 'mdx') && file.mimeType.startsWith('text/')) return 'markdown'
  if (rasterTypes[extension] === file.mimeType) return 'image'
  if ((file.mimeType.startsWith('text/') || file.mimeType === 'application/json') && textExtensions.has(extension)) return 'text'
  return 'unsupported'
}

function fileTree(files: SkillFile[]): FileNode[] {
  const roots: FileNode[] = []
  for (const file of files) {
    let level = roots
    let key = ''
    const parts = file.path.split('/')
    for (const [index, part] of parts.entries()) {
      key = key ? `${key}/${part}` : part
      let node = level.find((item) => item.key === key)
      if (!node) {
        const isLeaf = index === parts.length - 1
        node = { key, title: part, isLeaf, icon: isLeaf ? <FileTextOutlined /> : <FolderOpenOutlined />,
          ...(isLeaf ? {} : { children: [] }) }
        level.push(node)
      }
      level = node.children ?? []
    }
  }
  return roots
}

function SkillMarkdown({ value }: { value: string }) {
  return <div className="skills-detail-markdown">
    <ReactMarkdown skipHtml components={{
      img: ({ alt }) => <span className="skills-detail-image-note">[图片：{alt || '未命名'}，请在文件页查看]</span>,
      a: ({ href, children }) => href && /^https?:\/\//i.test(href)
        ? <a href={href} target="_blank" rel="noopener noreferrer">{children}</a> : <span>{children}</span>,
    }}>{value}</ReactMarkdown>
  </div>
}

function MarkdownBody(value: string) {
  return value.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n/, '')
}

function FilePreview({ file, skillId, versionId, mode }: {
  file: SkillFile; skillId: string; versionId: string; mode: PreviewMode
}) {
  const kind = fileKind(file)
  const content = useQuery({ queryKey: ['enterprise-skill-preview', skillId, versionId, file.path],
    queryFn: async () => {
      const blob = await readSkillVersionBlob(skillId, versionId, file.path)
      return kind === 'image' ? { blob } : { text: await blob.text() }
    }, enabled: kind !== 'unsupported' })
  const [imageUrl, setImageUrl] = useState('')
  useEffect(() => {
    if (kind !== 'image' || !content.data || !('blob' in content.data) || !content.data.blob) return
    const url = URL.createObjectURL(new Blob([content.data.blob], { type: file.mimeType }))
    setImageUrl(url)
    return () => URL.revokeObjectURL(url)
  }, [content.data, file.mimeType, kind])

  if (kind === 'unsupported') return <Empty description="此格式暂不支持在线预览，请在导入前检查原文件" />
  if (content.isLoading) return <Skeleton active paragraph={{ rows: 8 }} />
  if (content.isError) return <Alert type="error" showIcon message={errorText(content.error)} />
  if (kind === 'image') return imageUrl ? <img className="skills-detail-image" src={imageUrl} alt={file.path} /> : <Skeleton.Image active />
  const text = content.data && 'text' in content.data ? content.data.text ?? '' : ''
  if (mode === '源码') return <pre className="skills-detail-source">{text}</pre>
  if (kind === 'markdown') return <SkillMarkdown value={file.path === 'SKILL.md' ? MarkdownBody(text) : text} />
  if (file.path.toLowerCase().endsWith('.json')) {
    try { return <pre className="skills-detail-source">{JSON.stringify(JSON.parse(text), null, 2)}</pre> } catch { /* 保留原文 */ }
  }
  return <pre className="skills-detail-source skills-detail-plain">{text}</pre>
}

export function SkillDetailPage() {
  const { skillId } = useParams<{ skillId: string }>()
  const navigate = useNavigate()
  const location = useLocation()
  const returnTo = typeof location.state?.returnTo === 'string' && /^\/ai-resources\/skills(?:\?[^#]*)?$/.test(location.state.returnTo)
    ? location.state.returnTo : '/ai-resources/skills'
  const [versionId, setVersionId] = useState<string | null>(null)
  const [filePath, setFilePath] = useState<string | null>(null)
  const [previewMode, setPreviewMode] = useState<PreviewMode>('预览')
  const [introMode, setIntroMode] = useState<PreviewMode>('预览')
  const skills = useQuery({ queryKey: ['enterprise-skills'], queryFn: listSkills })
  const versions = useQuery({ queryKey: ['enterprise-skill-versions', skillId],
    queryFn: () => listSkillVersions(skillId!), enabled: Boolean(skillId) })
  const skill = skills.data?.find((item) => item.id === skillId)
  const selectedVersion = versions.data?.find((item) => item.id === versionId)
    ?? versions.data?.find((item) => item.id === skill?.publishedVersionId)
    ?? versions.data?.[0]
  const files = selectedVersion?.files ?? []
  const selectedFile = files.find((file) => file.path === filePath) ?? files.find((file) => file.path === 'SKILL.md') ?? files[0]
  const introFile = files.find((file) => file.path === 'SKILL.md')
  const intro = useQuery({ queryKey: ['enterprise-skill-intro', skillId, selectedVersion?.id],
    queryFn: () => readSkillVersionBlob(skillId!, selectedVersion!.id, 'SKILL.md').then((blob) => blob.text()),
    enabled: Boolean(skillId && selectedVersion && introFile) })

  return <div className="page-stack skills-detail-page">
    <div className="skills-detail-back"><Button type="text" icon={<ArrowLeftOutlined />}
      onClick={() => navigate(returnTo)}>返回 Skills 管理</Button></div>
    {skills.isLoading || versions.isLoading ? <Card><Skeleton active paragraph={{ rows: 10 }} /></Card> :
      skills.isError || versions.isError ? <Alert type="error" showIcon message={errorText(skills.error ?? versions.error)} /> :
        !skill ? <Card><Empty description="找不到这个 Skill"><Link to={returnTo}>返回 Skills 管理</Link></Empty></Card> :
          <Card className="skills-detail-card" variant="borderless">
            <div className="skills-detail-heading">
              <div>
                <Typography.Text className="skills-detail-eyebrow">企业 Skill</Typography.Text>
                <h1>{skill.name}</h1>
                <p>{skill.description}</p>
              </div>
              <Space size={8} wrap>
                <Tag color={skill.status === 'published' ? 'green' : 'default'}>
                  {skill.status === 'published' ? '已发布' : skill.status === 'disabled' ? '已停用' : '草稿'}</Tag>
                {selectedVersion && <Select aria-label="选择 Skill 版本" value={selectedVersion.id} style={{ minWidth: 130 }}
                  onChange={(value) => { setVersionId(value); setFilePath(null); setPreviewMode('预览'); setIntroMode('预览') }}
                  options={versions.data?.map((version) => ({ value: version.id,
                    label: `v${version.versionNo}${version.id === skill.publishedVersionId ? ' · 当前发布' : ''}` }))} />}
              </Space>
            </div>
            <Tabs className="skills-detail-tabs" items={[
              { key: 'intro', label: 'Skill 介绍', children: <div className="skills-detail-intro">
                <div className="skills-detail-pane-head"><strong>SKILL.md</strong>
                  <Segmented value={introMode} onChange={(value) => setIntroMode(value as PreviewMode)} options={['预览', '源码']} /></div>
                {intro.isLoading ? <Skeleton active /> : intro.isError ? <Alert type="error" showIcon message={errorText(intro.error)} /> :
                  intro.data ? introMode === '源码' ? <pre className="skills-detail-source">{intro.data}</pre>
                    : <SkillMarkdown value={MarkdownBody(intro.data)} /> : <Empty description="此版本没有 SKILL.md" />}
              </div> },
              { key: 'files', label: `Skill 文件 (${files.length})`, children: <div className="skills-detail-files">
                <aside className="skills-detail-tree" aria-label="Skill 文件列表">
                  <div className="skills-detail-tree-title">文件目录 <span>{files.length} 个文件</span></div>
                  <Tree key={selectedVersion?.id} showIcon defaultExpandAll blockNode treeData={fileTree(files)}
                    selectedKeys={selectedFile ? [selectedFile.path] : []}
                    onSelect={(keys) => { if (keys.length && files.some((file) => file.path === keys[0])) {
                      setFilePath(String(keys[0])); setPreviewMode('预览')
                    } }} />
                </aside>
                <div className="skills-detail-file-content">
                  {selectedFile && selectedVersion ? <>
                    <div className="skills-detail-pane-head">
                      <div className="skills-detail-file-title">
                        {fileKind(selectedFile) === 'image' ? <FileImageOutlined /> : <FileTextOutlined />}
                        <strong>{selectedFile.path}</strong><span>{selectedFile.size.toLocaleString()} B</span>
                      </div>
                      {fileKind(selectedFile) !== 'image' && fileKind(selectedFile) !== 'unsupported' &&
                        <Segmented value={previewMode} onChange={(value) => setPreviewMode(value as PreviewMode)} options={['预览', '源码']} />}
                    </div>
                    <FilePreview key={`${selectedVersion.id}/${selectedFile.path}`} file={selectedFile}
                      skillId={skill.id} versionId={selectedVersion.id} mode={previewMode} />
                  </> : <Empty description="此版本暂无文件" />}
                </div>
              </div> },
            ]} />
          </Card>}
  </div>
}
