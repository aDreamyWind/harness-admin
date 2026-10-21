import { CopyOutlined } from '@ant-design/icons'
import { useMutation } from '@tanstack/react-query'
import { Alert, Button, Collapse, Form, Input, InputNumber, Segmented, Select, Space, Tag, Typography, message } from 'antd'
import { useState } from 'react'

import { testMCPTool, type MCPTool } from '../api/mcp'
import { MarkdownContent } from './MarkdownContent'

type SchemaProperty = { type?: string; description?: string; enum?: unknown[]; default?: unknown }
type ToolSchema = { properties?: Record<string, SchemaProperty>; required?: string[] }

function parseObject(raw: string): Record<string, unknown> {
  const parsed: unknown = JSON.parse(raw)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('参数必须是 JSON 对象')
  return parsed as Record<string, unknown>
}

function readableJSON(value: unknown) {
  return JSON.stringify(value, null, 2)
}

function parsedContent(text: string): { value: unknown; isJSON: boolean } {
  try { return { value: JSON.parse(text) as unknown, isJSON: true } } catch { return { value: text, isJSON: false } }
}

function StructuredJSON({ value }: { value: unknown }) {
  if (Array.isArray(value) && value.length > 0 && value.every((item) => item && typeof item === 'object' && !Array.isArray(item))) {
    return <div className="mcp-result-records">
      <div className="mcp-result-count">共 {value.length} 条记录</div>
      {value.slice(0, 50).map((entry, index) => {
        const item = entry as Record<string, unknown>
        const title = item.name ?? item.title ?? item.id
        return <details key={index} open={value.length === 1 || index === 0}>
          <summary>{typeof title === 'string' || typeof title === 'number' ? String(title) : `第 ${index + 1} 条`}</summary>
          <div className="mcp-result-record-body">{Object.entries(item).filter(([key]) => key !== 'name' && key !== 'title').map(([key, field]) =>
            <div className="mcp-result-field" key={key}><span>{key}</span>{typeof field === 'string' && key === 'description'
              ? <MarkdownContent value={field} />
              : typeof field === 'string' || typeof field === 'number' || typeof field === 'boolean'
                ? <div className="mcp-result-text">{String(field)}</div>
                : <pre className="mcp-result-code">{readableJSON(field)}</pre>}</div>)}</div>
        </details>
      })}
      {value.length > 50 && <div className="mcp-result-count">仅展示前 50 条；可在「原始响应」查看全部。</div>}
    </div>
  }
  return <pre className="mcp-result-code">{readableJSON(value)}</pre>
}

function ResultContent({ value }: { value: unknown }) {
  const [view, setView] = useState<'内容' | '原始响应'>('内容')
  const result = value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null
  const content = Array.isArray(result?.content) ? result.content as Record<string, unknown>[] : []

  return <div className="mcp-result">
    <div className="mcp-result-head"><strong>调用结果</strong><Space>
      {result?.isError === true && <Tag color="error">工具返回错误</Tag>}
      <Segmented size="small" value={view} onChange={(next) => setView(next as typeof view)} options={['内容', '原始响应']} />
    </Space></div>
    {view === '原始响应' || content.length === 0
      ? <pre className="mcp-result-code">{readableJSON(value)}</pre>
      : <>
        {content.map((item, index) => <div className="mcp-result-item" key={index}>
          <div className="mcp-result-item-head"><span>{item.type === 'text' ? `文本内容 ${index + 1}` : `${String(item.type ?? '内容')} ${index + 1}`}</span>
            {typeof item.text === 'string' && <Button size="small" type="text" icon={<CopyOutlined />}
              onClick={() => navigator.clipboard.writeText(item.text as string).then(() => message.success('已复制内容')).catch(() => message.error('复制失败'))}>复制</Button>}</div>
          {typeof item.text === 'string' ? (() => {
            const parsed = parsedContent(item.text)
            return parsed.isJSON
              ? <StructuredJSON value={parsed.value} />
              : <div className="mcp-result-text">{item.text}</div>
          })() : item.type === 'image' && typeof item.data === 'string' && /^image\/(png|jpeg|gif|webp)$/.test(String(item.mimeType))
            ? <img className="mcp-result-image" src={`data:${item.mimeType};base64,${item.data}`} alt="工具返回的图片" />
            : <pre className="mcp-result-code">{readableJSON(item)}</pre>}
        </div>)}
        {result?._meta != null && <Collapse ghost size="small" items={[{ key: 'meta', label: '响应元信息', children: <pre className="mcp-result-code">{readableJSON(result._meta)}</pre> }]} />}
      </>}
  </div>
}

export function MCPToolTestPanel({ serviceId, tool, connectionId }: { serviceId: string; tool: MCPTool; connectionId?: string }) {
  const [form] = Form.useForm<Record<string, unknown>>()
  const [mode, setMode] = useState<'表单' | 'JSON'>('表单')
  const [raw, setRaw] = useState('{}')
  const [result, setResult] = useState<unknown>(null)
  const schema = tool.inputSchema as ToolSchema
  const properties = schema.properties && typeof schema.properties === 'object' ? Object.entries(schema.properties) : []
  const required = new Set(Array.isArray(schema.required) ? schema.required : [])
  const hasForm = properties.length > 0 && properties.length <= 30
  const displayMode = hasForm ? mode : 'JSON'
  const test = useMutation({
    mutationFn: (args: Record<string, unknown>) => testMCPTool(serviceId, tool.name, args, connectionId),
    onSuccess: setResult,
    onError: (error) => message.error(error instanceof Error ? error.message : '工具调用失败'),
  })

  function submit(values: Record<string, unknown>) {
    try {
      const args: Record<string, unknown> = {}
      for (const [name, property] of properties) {
        const value = values[name]
        if (value === undefined || value === null || value === '') continue
        args[name] = property.type === 'object' || property.type === 'array' ? JSON.parse(String(value)) as unknown : value
      }
      test.mutate(args)
    } catch { message.error('对象和数组参数需要填写有效的 JSON') }
  }

  return <div className="mcp-test-panel" key={tool.name}>
    <div className="mcp-test-head"><strong>测试：{tool.name}</strong><Segmented size="small" value={displayMode}
      onChange={(next) => setMode(next as typeof mode)} options={hasForm ? ['表单', 'JSON'] : ['JSON']} /></div>
    {tool.description && <p className="mcp-test-description">{tool.description}</p>}
    <Alert type="warning" showIcon message="测试会真实调用工具，可能修改业务数据。" />
    {displayMode === '表单' && hasForm ? <Form form={form} layout="vertical" onFinish={submit} className="mcp-test-form">
      {properties.map(([name, property]) => <Form.Item key={name} name={name}
        label={<span><strong>{name}</strong><Tag className="mcp-param-type">{property.type ?? '任意'}</Tag></span>}
        extra={property.description} rules={required.has(name) ? [{ required: true, message: `请填写 ${name}` }] : []}>
        {property.enum?.length ? <Select options={property.enum.map((option) => ({ label: String(option), value: option as string | number }))} />
          : property.type === 'boolean' ? <Select allowClear options={[{ label: '是', value: true }, { label: '否', value: false }]} />
            : property.type === 'number' || property.type === 'integer' ? <InputNumber style={{ width: '100%' }} step={property.type === 'integer' ? 1 : undefined} />
              : property.type === 'object' || property.type === 'array' ? <Input.TextArea rows={3} placeholder={property.type === 'array' ? '[]' : '{}'} />
                : <Input placeholder={property.default == null ? undefined : String(property.default)} />}
      </Form.Item>)}
      <Button type="primary" htmlType="submit" loading={test.isPending}>执行测试</Button>
    </Form> : <div className="mcp-test-json">
      <Typography.Text type="secondary">填写工具参数 JSON 对象</Typography.Text>
      <Input.TextArea value={raw} onChange={(event) => setRaw(event.target.value)} autoSize={{ minRows: 4, maxRows: 12 }} aria-label="工具参数 JSON" />
      <Button type="primary" loading={test.isPending} onClick={() => {
        try { test.mutate(parseObject(raw)) } catch (error) { message.error(error instanceof Error ? error.message : 'JSON 格式不正确') }
      }}>执行测试</Button>
    </div>}
    <Collapse ghost size="small" items={[{ key: 'schema', label: '查看完整输入结构', children: <pre className="mcp-result-code">{readableJSON(tool.inputSchema)}</pre> }]} />
    {result !== null && <ResultContent value={result} />}
  </div>
}
