import { Alert, Button, Form, Input, message, Modal, Radio, Select, Typography } from 'antd'
import { useEffect, useState } from 'react'

import { inspectMCPRepository, type MCPDeploymentInput, type MCPSourceInspection } from '../api/mcpDeployments'

type FormValues = Omit<MCPDeploymentInput, 'archive'>

interface Props {
  open: boolean
  isVersion?: boolean
  initialValues?: Partial<FormValues>
  loading: boolean
  onClose: () => void
  onSubmit: (input: MCPDeploymentInput) => void
}

export function MCPDeploymentDialog({ open, isVersion = false, initialValues, loading, onClose, onSubmit }: Props) {
  const [form] = Form.useForm<FormValues>()
  const sourceType = Form.useWatch('sourceType', form)
  const runtimeType = Form.useWatch('runtimeType', form)
  const [archive, setArchive] = useState<File | undefined>()
  const [inspection, setInspection] = useState<MCPSourceInspection | undefined>()
  const [inspecting, setInspecting] = useState(false)
  useEffect(() => {
    if (open) {
      form.resetFields()
      form.setFieldsValue({ sourceType: 'repository', runtimeType: 'node', ...initialValues })
      setArchive(undefined)
      setInspection(undefined)
    }
  }, [open, form])

  return <Modal title={isVersion ? '构建新版本' : '从代码部署 MCP'} open={open} width={680}
    okText="开始构建" okButtonProps={{ loading }} onOk={() => form.submit()}
    onCancel={onClose} cancelButtonProps={{ disabled: loading }} closable={!loading} maskClosable={!loading}>
    <Alert type="info" showIcon style={{ marginBottom: 16 }}
      message="Node.js 项目可自动识别 package.json 中的运行入口，并在隔离构建环境中安装依赖、执行 build 脚本。正式运行容器默认断网；不会执行仓库自带 Dockerfile。" />
    <Form form={form} layout="vertical" onValuesChange={(changed) => {
      if ('repositoryUrl' in changed || 'ref' in changed || 'sourceType' in changed) setInspection(undefined)
    }} onFinish={(values) => {
      if (values.sourceType === 'upload' && !archive) {
        form.setFields([{ name: 'sourceType', errors: ['请选择 .tar.gz 代码包'] }])
        return
      }
      onSubmit({ ...values, archive })
    }}>
      {!isVersion && <Form.Item name="name" label="服务名称" rules={[{ required: true, message: '请填写服务名称' }]}>
        <Input maxLength={120} />
      </Form.Item>}
      <Form.Item name="sourceType" label="代码来源" rules={[{ required: true }]}>
        <Radio.Group optionType="button" buttonStyle="solid" options={[
          { label: 'GitHub / Gitee 仓库', value: 'repository' }, { label: '上传代码包', value: 'upload' },
        ]} />
      </Form.Item>
      {sourceType === 'repository' ? <>
        <Form.Item name="repositoryUrl" label="公开仓库地址" rules={[{ required: true, message: '请填写完整的 GitHub 或 Gitee 仓库地址' }, { pattern: /^https:\/\/(github\.com|gitee\.com)\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+(?:\.git)?\/?$/, message: '请填写 https://github.com/组织/仓库 或 https://gitee.com/组织/仓库' }]}>
          <Input placeholder="https://github.com/组织/仓库" />
        </Form.Item>
        <Form.Item name="ref" label="分支、标签或提交（可留空）" extra={isVersion
          ? '已沿用上次配置。失败版本默认重试原提交；清空后使用仓库默认分支的最新提交。'
          : '留空使用仓库默认分支；构建时自动解析并记录固定提交号。'}>
          <Input placeholder="例如 main、v1.0.0 或提交号" />
        </Form.Item>
        {runtimeType === 'node' && <>
          <Button loading={inspecting} disabled={loading} onClick={async () => {
            try {
              const { repositoryUrl } = await form.validateFields(['repositoryUrl'])
			  if (!isVersion && !String(form.getFieldValue('name') ?? '').trim()) {
				const repositoryName = repositoryUrl!.replace(/\/$/, '').split('/').pop()?.replace(/\.git$/, '')
				if (repositoryName) form.setFieldValue('name', repositoryName)
			  }
              setInspecting(true)
              const plan = await inspectMCPRepository(repositoryUrl!, form.getFieldValue('ref'))
              setInspection(plan)
              form.setFieldValue('ref', plan.revision)
            } catch (error) {
              if (error instanceof Error) message.error(error.message)
            } finally {
              setInspecting(false)
            }
          }}>识别运行方式</Button>
          {inspection && <Alert type="success" showIcon style={{ marginTop: 12, marginBottom: 16 }}
            message={`已识别：${inspection.buildCommand || '无需编译'} → ${inspection.entryFile}`}
            description={inspection.hasLockfile ? '已找到依赖锁文件；构建将使用固定依赖。' : '仓库未提交依赖锁文件，重新构建可能得到不同的依赖版本。'} />}
        </>}
      </> : <div className="mcp-deployment-upload">
        <Typography.Text>代码包（.tar.gz，不超过 20 MiB）</Typography.Text>
        <input type="file" accept=".tar.gz,application/gzip" aria-label="上传 MCP 代码包"
          onChange={(event) => setArchive(event.target.files?.[0])} />
        {archive && <Typography.Text type="secondary">已选：{archive.name}</Typography.Text>}
      </div>}
      <Form.Item name="runtimeType" label="运行环境" rules={[{ required: true }]}>
        <Select options={[{ label: 'Node.js', value: 'node' }, { label: 'Python', value: 'python' }]} />
      </Form.Item>
      <Form.Item name="entryFile" label={runtimeType === 'python' ? 'Python 运行入口文件' : '运行入口文件（可留空自动识别）'}
        extra={runtimeType === 'python' ? '填写代码包中的 .py 相对路径。' : '留空时读取 package.json 的 start 或 main；先构建，再验证生成的 .js 文件。无法识别时才需手动指定。'}
        rules={[{ required: runtimeType === 'python', message: '请填写 Python 运行入口文件' }]}>
        <Input placeholder={runtimeType === 'python' ? 'src/server.py' : '自动识别，例如 build/index.js'} />
      </Form.Item>
      <Typography.Text type="secondary">服务描述读取根目录 README.md。Node.js 若无 package-lock.json，构建时会生成锁文件，重新构建可能得到不同依赖版本；安装依赖需要运维启用受控构建网络。Python 仅安装 requirements.txt 中可用的 wheel 包。</Typography.Text>
    </Form>
  </Modal>
}
