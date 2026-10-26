import assert from 'node:assert/strict'
import { test } from 'node:test'

import { ApiError, csrfHeaders, onSessionExpired, platformCSRFHeaders, request, requestBlob } from '../src/api/client.ts'

test('受保护接口返回 401 时通知对应的登录域', async (t) => {
  const scopes = []
  const unsubscribe = onSessionExpired((scope) => scopes.push(scope))
  t.after(unsubscribe)
  t.mock.method(globalThis, 'fetch', async () => Response.json({ error: {
    code: 'unauthorized', message: '登录状态已失效，请重新登录',
  } }, { status: 401 }))

  await assert.rejects(request('/api/v1/skills'), (error) => error instanceof ApiError && error.status === 401)
  await assert.rejects(request('/api/v1/platform/organizations'), (error) => error instanceof ApiError && error.status === 401)
  await assert.rejects(requestBlob('/api/v1/skills/id/versions/id/download', 'application/zip', '下载失败'),
    (error) => error instanceof ApiError && error.status === 401)
  assert.deepEqual(scopes, ['organization', 'platform', 'organization'])
})

test('登录失败和非 401 错误不触发会话过期', async (t) => {
  const scopes = []
  const unsubscribe = onSessionExpired((scope) => scopes.push(scope))
  t.after(unsubscribe)
  t.mock.method(globalThis, 'fetch', async () => Response.json({ error: {
    code: 'invalid_credentials', message: '账号或密码错误',
  } }, { status: 401 }))

  await assert.rejects(request('/api/v1/auth/login', { method: 'POST' }))
  await assert.rejects(request('/api/v1/platform/auth/login', { method: 'POST' }))
  t.mock.restoreAll()
  t.mock.method(globalThis, 'fetch', async () => Response.json({ error: {
    code: 'forbidden', message: '没有权限',
  } }, { status: 403 }))
  await assert.rejects(request('/api/v1/skills'))
  assert.deepEqual(scopes, [])
})

test('安全凭据过期时也通知重新登录', (t) => {
  const scopes = []
  const unsubscribe = onSessionExpired((scope) => scopes.push(scope))
  t.after(unsubscribe)
  const originalDocument = globalThis.document
  globalThis.document = { cookie: '' }
  t.after(() => {
    if (originalDocument === undefined) delete globalThis.document
    else globalThis.document = originalDocument
  })

  assert.throws(csrfHeaders, (error) => error instanceof ApiError && error.status === 401)
  assert.throws(platformCSRFHeaders, (error) => error instanceof ApiError && error.status === 401)
  assert.deepEqual(scopes, ['organization', 'platform'])
})
