import assert from 'node:assert/strict'
import { test } from 'node:test'

import { findRepeatedIdeaSuggestion, ideaSuggestionExcerpts, matchingIdeaSuggestions } from '../src/pages/novelIdeaHistory.ts'

const source = '古代、穿越流、系统流、爽文，主角是闲散王爷，和太子皇子们交好，群像风，语态用词都要古代的。'
const similarSource = '古代穿越流系统流爽文，主角是闲散王爷，与太子皇子们交好，写成群像风，用词保留古代语感。'

test('避重历史优先选择相同灵感，再选近期相近结果', () => {
  const history = [
    { id: 'other', savedAt: 7, source: '古代穿越流系统流爽文，女侠护送边塞军医，卷入盐商私斗。', result: '无关的结果' },
    { id: 'specified', savedAt: 6, source, instruction: '只润色', result: '按指定要求改写的结果' },
    { id: 'pending', savedAt: 5, source },
    { id: 'unchanged', savedAt: 4, source, result: source },
    { id: 'similar', savedAt: 3, source: similarSource, instruction: '  ', result: '王爷随太子出城查验旧案' },
    { id: 'exact', savedAt: 2, source: source.replace(/[、，。]/g, ' '), result: '王爷在宫宴上收到一封信' },
  ]

  assert.deepEqual(matchingIdeaSuggestions(history, source), ['王爷在宫宴上收到一封信', '王爷随太子出城查验旧案'])
})

test('避重历史按完整 Unicode 字符截取开头，同一片段只发送一次', () => {
  const excerpt = '𠮷'.repeat(500)
  const history = [`${excerpt}甲`, `${excerpt}乙`, '宫宴', '密信', '旧案'].map((result, index) => ({
    id: String(index), savedAt: 5 - index, source, result,
  }))

  const matched = matchingIdeaSuggestions(history, source)
  assert.equal(matched.length, 5)
  const suggestions = ideaSuggestionExcerpts(matched)
  assert.deepEqual(suggestions, [excerpt, '宫宴', '密信'])
  assert.equal(Array.from(suggestions[0]).length, 500)
})

test('前面三条之外的旧结果仍参与重名检查，其他题材不阻断', () => {
  const old = `${'由礼部主事将密信送出城门。'.repeat(50)}主角是萧景珩。`
  const history = ['昨日王爷查私盐', '李云渡河救人', '王爷在御苑撞见刺客', '王爷修缮祖宅', old, '主角沈明靠修船为生。']
    .map((result, index) => ({ id: String(index), savedAt: 10 - index, source: index < 5 ? source : '海上修船的日常纪实', result }))
  const matched = matchingIdeaSuggestions(history, source)

  assert.deepEqual(findRepeatedIdeaSuggestion(matched, source, { brief: '萧景珩重整旧案。', protagonistName: '萧景珩' }), {
    suggestion: old, name: '萧景珩',
  })
  assert.equal(findRepeatedIdeaSuggestion(matched, source, { brief: '沈明擅长种茶。', protagonistName: '沈明' }), undefined)
})

test('系统名只改通用后缀仍算重复，作者指定的专名可以保留', () => {
  const results = ['主角林砚借助金枝司系统查出粮税舞弊。']
  const candidate = { brief: '林砚打开「金枝司助手」，开始修复被盗的石碑。', protagonistName: '林砚', systemName: '金枝司助手' }

  assert.deepEqual(findRepeatedIdeaSuggestion(results, `${source} 主角叫林砚。`, candidate), {
    suggestion: results[0], name: '金枝司',
  })
  assert.equal(findRepeatedIdeaSuggestion(results, `${source} 主角叫林砚，系统叫金枝司。`, candidate), undefined)
})

test('旧结果没有命名元数据时也能识别照抄和只换人名的开场', () => {
  const old = '林砚从木棺里惊醒，发现从前的王爷从未离开过陵县。送信人赶在宵禁前敲开院门，递上装着失踪官员私印的绢袋，恳请他藏起其中的绝笔。'
  const results = [old]

  assert.deepEqual(findRepeatedIdeaSuggestion(results, source, { brief: old.replaceAll('，', ' ') }), { suggestion: old })
  assert.deepEqual(findRepeatedIdeaSuggestion(results, source, { brief: old.replace('林砚', '江若'), protagonistName: '江若' }), { suggestion: old })
  assert.equal(findRepeatedIdeaSuggestion(results, `${source} 开场如下：${old}`, { brief: old }), undefined)
})
