import assert from 'node:assert/strict'
import { test } from 'node:test'

import { createCanvasHistory } from '../src/pages/drama-canvas-history.ts'

test('画布历史连续撤销和重做，新的编辑会结束旧重做分支', () => {
  const history = createCanvasHistory()
  history.push('初始')
  history.push('添加节点')

  assert.equal(history.undo('连接节点'), '添加节点')
  assert.equal(history.undo('添加节点'), '初始')
  assert.equal(history.canUndo, false)
  assert.equal(history.redo('初始'), '添加节点')
  assert.equal(history.canRedo, true)

  history.push('添加节点')
  assert.equal(history.canRedo, false)
  assert.equal(history.redo('编辑节点'), undefined)
  assert.equal(history.undo('编辑节点'), '添加节点')
})

test('画布只保留最近四十步，清空后不能撤销或重做', () => {
  const history = createCanvasHistory()
  for (let position = 0; position < 41; position++) history.push(position)

  let position = 41
  for (let expected = 40; expected >= 1; expected--) {
    position = history.undo(position)
    assert.equal(position, expected)
  }
  assert.equal(history.undo(position), undefined)
  assert.equal(history.canRedo, true)

  history.clear()
  assert.equal(history.canUndo, false)
  assert.equal(history.canRedo, false)
})
