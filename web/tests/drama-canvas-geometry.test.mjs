import assert from 'node:assert/strict'
import { test } from 'node:test'

import { arrangeCanvasNodes, findCanvasNodePosition, revealCanvasNode } from '../src/pages/drama-canvas-geometry.ts'

function node(id, group, width, height, x = 0, y = 0) {
  return { id, group, width, height, position: { x, y } }
}

function separated(a, aPosition, b, bPosition) {
  return aPosition.x + a.width <= bPosition.x || bPosition.x + b.width <= aPosition.x ||
    aPosition.y + a.height <= bPosition.y || bPosition.y + b.height <= aPosition.y
}

test('整理时场景末列、道具首列与媒体使用各自的尺寸，避免相互覆盖', () => {
  const nodes = [
    node('scene-1', 'scene', 400, 320), node('scene-2', 'scene', 550, 360), node('scene-3', 'scene', 400, 300),
    node('scene-4', 'scene', 400, 320), node('prop', 'prop', 280, 410), node('image', 'image', 670, 600),
  ]
  const positions = arrangeCanvasNodes(nodes)
  assert.equal(positions.size, nodes.length)
  for (const [index, first] of nodes.entries()) {
    for (const second of nodes.slice(index + 1)) {
      assert.ok(separated(first, positions.get(first.id), second, positions.get(second.id)), `${first.id} 与 ${second.id} 重叠`)
    }
  }
  assert.ok(positions.get('scene-4').y >= nodes[1].height)
})

test('优先在当前视口内找空位，中心堵住时也能放到左边', () => {
  const size = { width: 100, height: 100 }
  const occupied = node('block', 'image', 400, 100, 200, 0)
  const visible = { x: 0, y: 0, width: 600, height: 100 }
  const position = findCanvasNodePosition({ x: 250, y: 0 }, size, [occupied], visible)
  assert.ok(position.x >= 0 && position.x + size.width <= visible.width)
  assert.equal(position.y, 0)
  assert.ok(separated(size, position, occupied, occupied.position))
})

test('只平移足以显示裁切节点的距离，并保留缩放', () => {
  const viewport = { x: 10, y: 30, zoom: 0.5 }
  const safeArea = { x: 20, y: 30, width: 300, height: 200 }
  assert.equal(revealCanvasNode({ x: 60, y: 40, width: 200, height: 160 }, viewport, safeArea), viewport)
  assert.deepEqual(revealCanvasNode({ x: 500, y: 40, width: 200, height: 160 }, viewport, safeArea), { x: -30, y: 30, zoom: 0.5 })
})
