import { CopyOutlined, EyeInvisibleOutlined, EyeOutlined } from '@ant-design/icons'
import { Alert, Button, Input, Modal } from 'antd'
import { useState } from 'react'

import { ApiError } from '../api/client'

export function SecretRevealModal({ title, label, requestSecret, onClose, onCopy }: {
  title: string
  label: string
  requestSecret: () => Promise<{ secret: string }>
  onClose: () => void
  onCopy: () => void
}) {
  const [secret, setSecret] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const toggleSecret = async () => {
    setError('')
    if (secret) {
      setSecret('')
      return
    }
    setLoading(true)
    try {
      const result = await requestSecret()
      setSecret(result.secret)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : `${label}查看失败`)
    } finally {
      setLoading(false)
    }
  }

  const copySecret = async () => {
    try {
      await navigator.clipboard.writeText(secret)
      setError('')
      onCopy()
    } catch {
      setError('复制失败，请选中密钥后手动复制')
    }
  }

  return (
    <Modal title={title} open width={800} footer={null} onCancel={onClose}>
      <Alert type={error ? 'error' : 'info'} showIcon
        title={error || (secret ? '本次查看已记录到审计日志' : '点击眼睛显示密钥，查看操作会记录到审计日志。')} />
      <div className="credential-value-row">
        <Input aria-label={label} readOnly value={secret || '••••••••••••'} />
        <Button aria-label={secret ? '隐藏密钥' : '显示密钥'} icon={secret ? <EyeInvisibleOutlined /> : <EyeOutlined />}
          loading={loading} onClick={toggleSecret} />
        <Button icon={<CopyOutlined />} disabled={!secret} onClick={copySecret}>复制</Button>
      </div>
    </Modal>
  )
}
