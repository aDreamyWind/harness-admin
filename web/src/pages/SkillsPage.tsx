import { FileTextOutlined, GithubOutlined, InboxOutlined, PlusOutlined, ReloadOutlined, UploadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Collapse, Empty, Form, Input, Modal, Segmented, Select, Space, Spin, Table, Tag, Typography, Upload, message } from 'antd'
import { useState } from 'react'

import { useAuth } from '../auth/AuthContext'
import { listDepartments } from '../api/departments'
import { listUsers } from '../api/users'
import {
  importGitHubSkill, importSkill, listSkills, listSkillVersions, publishSkill, readSkillVersionFile, replaceSkillGrants,
  type SkillAsset, type SkillFile, type SkillVersion,
} from '../api/skills'
import { ModelPolicyDepartmentTree } from '../components/ModelPolicyDepartmentTree'

const errorText = (error: unknown) => error instanceof Error ? error.message : '操作失败，请重试'

export function SkillsPage() {
  const { hasPermission } = useAuth()
  const canWrite = hasPermission('skill.asset.write')
  const canReadUsers = hasPermission('iam.user.read')
  const canReadDepartments = hasPermission('iam.department.read')
  const queryClient = useQueryClient()
  const [messageApi, holder] = message.useMessage()
  const [versionTarget, setVersionTarget] = useState<SkillAsset | null>(null)
  const [grantTarget, setGrantTarget] = useState<SkillAsset | null>(null)
  const [departmentIds, setDepartmentIds] = useState<string[]>([])
  const [userIds, setUserIds] = useState<string[]>([])
  const [userSearch, setUserSearch] = useState('')
  const [preview, setPreview] = useState<{ name: string; text: string } | null>(null)
  const [importOpen, setImportOpen] = useState(false)
  const [importMode, setImportMode] = useState<'upload' | 'github'>('upload')
  const [selectedFile, setSelectedFile] = useState<File | null>(null)
  const [githubForm] = Form.useForm<{ repositoryUrl: string; ref: string; skillPath: string }>()
  const skills = useQuery({ queryKey: ['enterprise-skills'], queryFn: listSkills })
  const versions = useQuery({ queryKey: ['enterprise-skill-versions', versionTarget?.id],
    queryFn: () => listSkillVersions(versionTarget!.id), enabled: Boolean(versionTarget) })
  const departments = useQuery({ queryKey: ['departments'], queryFn: listDepartments, enabled: Boolean(grantTarget && canReadDepartments) })
  const users = useQuery({ queryKey: ['users', 'skill-grants', userSearch],
    queryFn: () => listUsers({ search: userSearch, page: 1, pageSize: 100 }), enabled: Boolean(grantTarget && canReadUsers) })

  const upload = useMutation({ mutationFn: importSkill, onSuccess: async (asset) => {
    await queryClient.invalidateQueries({ queryKey: ['enterprise-skills'] })
    setImportOpen(false); setSelectedFile(null)
    setVersionTarget(asset)
    messageApi.success('Skill 版本已导入，请检查文件后再发布')
  }, onError: (error) => messageApi.error(errorText(error)) })
  const githubImport = useMutation({ mutationFn: importGitHubSkill, onSuccess: async ({ asset, revision }) => {
    await queryClient.invalidateQueries({ queryKey: ['enterprise-skills'] })
    setImportOpen(false); githubForm.resetFields()
    setVersionTarget(asset)
    messageApi.success(`已从 GitHub 导入提交 ${revision.slice(0, 12)}，请检查文件后再发布`)
  }, onError: (error) => messageApi.error(errorText(error)) })

  async function submitImport() {
    if (importMode === 'upload') {
      if (!selectedFile) { messageApi.warning('请先选择 Skill 包'); return }
      upload.mutate(selectedFile)
      return
    }
    try {
      const fields = await githubForm.validateFields()
      githubImport.mutate({ repositoryUrl: fields.repositoryUrl.trim(), ref: (fields.ref ?? '').trim(),
        skillPath: (fields.skillPath ?? '').trim().replace(/^\/+|\/+$/g, '') })
    } catch { /* 表单会显示校验结果 */ }
  }
  const publish = useMutation({ mutationFn: (input: { id: string; status: 'published' | 'disabled'; versionId?: string }) =>
    publishSkill(input.id, input.status, input.versionId), onSuccess: async (asset) => {
    setVersionTarget((current) => current?.id === asset.id ? asset : current)
    queryClient.setQueryData<SkillAsset[]>(['enterprise-skills'], (current) =>
      current?.map((skill) => skill.id === asset.id ? asset : skill))
    await queryClient.invalidateQueries({ queryKey: ['enterprise-skills'] })
    messageApi.success('发布状态已更新')
  }, onError: (error) => messageApi.error(errorText(error)) })
  const saveGrants = useMutation({ mutationFn: () => replaceSkillGrants(grantTarget!.id, departmentIds, userIds),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['enterprise-skills'] })
      setGrantTarget(null)
      messageApi.success('授权已保存')
    }, onError: (error) => messageApi.error(errorText(error)) })

  async function showFile(version: SkillVersion, file: SkillFile) {
    if (!versionTarget) return
    try {
      const text = await readSkillVersionFile(versionTarget.id, version.id, file.path)
      setPreview({ name: `${versionTarget.name} / v${version.versionNo} / ${file.path}`, text })
    } catch (error) { messageApi.error(errorText(error)) }
  }

  return <div className="page-stack">
    {holder}
    <Card className="model-resource-card skills-card" variant="borderless">
      <div className="model-resource-toolbar skills-toolbar">
        <div>
          <strong>Skills 管理</strong>
          <span>从本地文件或 GitHub 导入 Skill，审阅版本后发布给指定部门或员工。</span>
        </div>
        <Space size={8}>
          <Button icon={<ReloadOutlined />} onClick={() => skills.refetch()}>刷新</Button>
          {canWrite && <Button type="primary" icon={<PlusOutlined />} onClick={() => setImportOpen(true)}>导入 Skill</Button>}
        </Space>
      </div>
      {skills.isError && <Alert className="model-resource-error" type="error" showIcon message={errorText(skills.error)}
        action={<Button size="small" onClick={() => skills.refetch()}>重试</Button>} />}
      <Table<SkillAsset> rowKey="id" dataSource={skills.data ?? []} loading={skills.isLoading}
        pagination={{ pageSize: 10, hideOnSinglePage: true }} scroll={{ x: 1120 }}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无企业 Skill，导入后可在这里审阅和发布" /> }} columns={[
          { title: 'Skill', dataIndex: 'name', align: 'center', width: 200,
            render: (name: string) => <strong className="skills-cell-text">{name}</strong> },
          { title: '用途说明', dataIndex: 'description', align: 'center', width: 330,
            render: (description: string) => <span className="skills-cell-text">{description}</span> },
          { title: '最新版本', dataIndex: 'latestVersionNo', align: 'center', width: 110, render: (value: number) => `v${value}` },
          { title: '状态', dataIndex: 'status', align: 'center', width: 110, render: (value: SkillAsset['status']) => <Tag color={value === 'published' ? 'green' : 'default'}>
            {value === 'published' ? '已发布' : value === 'disabled' ? '已停用' : '草稿'}</Tag> },
          { title: '授权范围', align: 'center', width: 180,
            render: (_: unknown, item: SkillAsset) => `${item.departmentIds.length} 个部门、${item.userIds.length} 名员工` },
          { title: '操作', align: 'center', width: 190, render: (_: unknown, item: SkillAsset) => <Space size={4} wrap>
            <Button type="link" icon={<FileTextOutlined />} onClick={() => { setPreview(null); setVersionTarget(item) }}>版本与文件</Button>
            {canWrite && <Button type="link" onClick={() => {
              setGrantTarget(item); setDepartmentIds(item.departmentIds); setUserIds(item.userIds)
            }}>授权</Button>}
            {canWrite && item.status === 'published' && <Button type="link" danger loading={publish.isPending}
              onClick={() => publish.mutate({ id: item.id, status: 'disabled' })}>停用</Button>}
          </Space> },
        ]} />
    </Card>

    <Modal title="导入 Skill" open={importOpen} width={620}
      onCancel={() => { if (!upload.isPending && !githubImport.isPending) setImportOpen(false) }}
      onOk={submitImport} okText="导入并创建草稿" okButtonProps={{ disabled: importMode === 'upload' && !selectedFile }}
      confirmLoading={upload.isPending || githubImport.isPending} destroyOnHidden>
      <Segmented block value={importMode} onChange={(value) => setImportMode(value as 'upload' | 'github')}
        options={[{ value: 'upload', label: '本地文件上传', icon: <UploadOutlined /> },
          { value: 'github', label: '从 GitHub 导入', icon: <GithubOutlined /> }]} style={{ marginBottom: 20 }} />
      {importMode === 'upload' ? <>
        <Upload.Dragger accept=".zip,.tar.gz,.tgz" maxCount={1} showUploadList={false}
          beforeUpload={(file: File) => { setSelectedFile(file); return false }}>
          <p className="ant-upload-drag-icon"><InboxOutlined /></p>
          <p className="ant-upload-text">{selectedFile ? selectedFile.name : '点击或拖入 .zip、.tar.gz Skill 包'}</p>
          <p className="ant-upload-hint">单个文件不超过 16 MiB；导入后可检查文件与版本</p>
        </Upload.Dragger>
      </> : <Form form={githubForm} layout="vertical" initialValues={{ ref: '', skillPath: '' }}>
        <Alert type="info" showIcon message="只支持公开 GitHub 仓库。导入时会将分支或标签固定到实际提交号。" style={{ marginBottom: 18 }} />
        <Form.Item name="repositoryUrl" label="GitHub 仓库地址" rules={[{ required: true, message: '请输入公开仓库地址' },
          { pattern: /^https:\/\/github\.com\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+(?:\.git)?\/?$/, message: '请输入 https://github.com/所有者/仓库' }]}>
          <Input placeholder="https://github.com/owner/repository" autoComplete="off" />
        </Form.Item>
        <Form.Item name="ref" label="分支、标签或提交号（可选）" extra="留空使用仓库默认分支">
          <Input placeholder="例如 main、v1.0.0 或完整提交号" autoComplete="off" />
        </Form.Item>
        <Form.Item name="skillPath" label="Skill 子目录（可选）" extra="仓库根目录有 SKILL.md 时留空；例如 skills/code-review">
          <Input placeholder="skills/code-review" autoComplete="off" />
        </Form.Item>
      </Form>}
    </Modal>

    <Modal title={`版本与文件：${versionTarget?.name ?? ''}`} open={Boolean(versionTarget)} width={920}
      onCancel={() => { setVersionTarget(null); setPreview(null) }} footer={<Button onClick={() => setVersionTarget(null)}>关闭</Button>}>
      <Alert type="info" showIcon style={{ marginBottom: 16 }} message="发布会立即替换员工可读的版本。请先核对 SKILL.md、脚本和文件摘要。" />
      <div className="skills-version-list">
        {versions.isLoading ? <div className="skills-version-loading"><Spin /></div> :
          versions.isError ? <Alert type="error" showIcon message={errorText(versions.error)} /> :
          versions.data?.length ? versions.data.map((version) => {
            const isPublished = versionTarget?.publishedVersionId === version.id && versionTarget?.status === 'published'
            return <Card key={version.id} size="small" className="skills-version-card">
              <div className="skills-version-header">
                <Space size={8} wrap>
                  <Tag color="blue">v{version.versionNo}</Tag>
                  {isPublished && <Tag color="green">当前发布</Tag>}
                  <Typography.Text type="secondary">{version.sourceType === 'github' ? 'GitHub 导入' : '本地上传'}</Typography.Text>
                </Space>
                {canWrite && <Button type={isPublished ? 'default' : 'primary'} disabled={isPublished}
                  loading={publish.isPending} onClick={() => publish.mutate({ id: versionTarget!.id, status: 'published', versionId: version.id })}>
                  {isPublished ? '当前发布' : '发布此版本'}</Button>}
              </div>
              <div className="skills-version-description">
                <span className="skills-version-label">用途说明</span>
                <Typography.Paragraph>{String(version.frontmatter.description ?? '')}</Typography.Paragraph>
              </div>
              <div className="skills-version-facts">
                <div><span className="skills-version-label">文件数量</span><strong>{version.files.length} 个</strong></div>
                <div><span className="skills-version-label">清单摘要</span>
                  <Typography.Text className="skills-digest" copyable={{ text: version.manifestDigest }}>{version.manifestDigest}</Typography.Text></div>
                {version.sourceType === 'github' && <>
                  <div><span className="skills-version-label">来源仓库</span>
                    <Typography.Text copyable={{ text: version.sourceRef }}>{version.sourceRef}</Typography.Text></div>
                  <div><span className="skills-version-label">固定提交</span>
                    <Typography.Text className="skills-digest" copyable={{ text: version.sourceRevision }}>{version.sourceRevision}</Typography.Text></div>
                  {version.sourcePath && <div><span className="skills-version-label">Skill 子目录</span><Typography.Text>{version.sourcePath}</Typography.Text></div>}
                </>}
              </div>
              <Collapse size="small" className="skills-version-files" items={[{ key: 'files', label: `查看文件清单（${version.files.length}）`,
                children: <Table<SkillFile> size="small" rowKey="path" pagination={false} scroll={{ x: 740 }}
                  dataSource={version.files} columns={[
                    { title: '文件', dataIndex: 'path', align: 'center', width: 300, render: (path: string, file: SkillFile) =>
                      file.mimeType.startsWith('text/') ? <Button type="link" onClick={() => showFile(version, file)}>{path}</Button> : path },
                    { title: '大小', dataIndex: 'size', align: 'center', width: 100, render: (value: number) => `${value} B` },
                    { title: 'SHA-256', dataIndex: 'digest', align: 'center', width: 340,
                      render: (digest: string) => <span className="skills-cell-text skills-digest">{digest}</span> },
                  ]} /> }]} />
            </Card>
          }) : <Empty description="暂无版本" />}
      </div>
      {preview && <Card size="small" title={preview.name} style={{ marginTop: 16 }}>
        <Input.TextArea readOnly value={preview.text} autoSize={{ minRows: 8, maxRows: 20 }} />
      </Card>}
    </Modal>

    <Modal title={`授权：${grantTarget?.name ?? ''}`} open={Boolean(grantTarget)} width={680}
      onCancel={() => setGrantTarget(null)} onOk={() => saveGrants.mutate()}
      okText="保存授权" confirmLoading={saveGrants.isPending}>
      <Form layout="vertical">
        <Form.Item label="授权部门">
          {canReadDepartments ? <ModelPolicyDepartmentTree departments={departments.data ?? []}
            selectedIds={departmentIds} onChange={setDepartmentIds} /> : <Typography.Text type="secondary">当前账号没有部门目录读取权限</Typography.Text>}
        </Form.Item>
        <Form.Item label="单独授权员工">
          {canReadUsers ? <Select mode="multiple" showSearch filterOption={false} onSearch={setUserSearch}
            value={userIds} onChange={setUserIds} style={{ width: '100%' }}
            options={(users.data?.items ?? []).map((user) => ({ label: `${user.displayName}（${user.username}）`, value: user.id }))} />
            : <Typography.Text type="secondary">当前账号没有用户目录读取权限</Typography.Text>}
        </Form.Item>
        <Typography.Text type="secondary">保存的是最终勾选结果；全部清空后员工将无法读取此 Skill。</Typography.Text>
      </Form>
    </Modal>
  </div>
}
