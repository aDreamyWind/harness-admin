import { Input, Tree, Typography } from 'antd'
import { useMemo, useState, type ReactNode } from 'react'

import type { Department } from '../api/departments'

const { Text } = Typography

type TreeNode = {
  key: string
  title: ReactNode
  children?: TreeNode[]
  disableCheckbox?: boolean
}

interface Props {
  departments: Department[]
  selectedIds: string[]
  onChange: (ids: string[]) => void
}

export function ModelPolicyDepartmentTree({ departments, selectedIds, onChange }: Props) {
  const [search, setSearch] = useState('')
  const byID = useMemo(() => new Map(departments.map((item) => [item.id, item])), [departments])
  const childrenByParent = useMemo(() => {
    const result = new Map<string, string[]>()
    for (const item of departments) {
      if (!item.parentId || !byID.has(item.parentId)) continue
      result.set(item.parentId, [...(result.get(item.parentId) ?? []), item.id])
    }
    return result
  }, [departments, byID])
  const treeData = useMemo(() => {
    const nodes = new Map<string, TreeNode>()
    const roots: TreeNode[] = []
    for (const item of departments) {
      nodes.set(item.id, {
        key: item.id,
        title: <span className="policy-department-label">
          <span>{item.name}</span>
          <small>直属 {item.userCount} 人</small>
        </span>,
        children: [],
        disableCheckbox: item.status !== 'active',
      })
    }
    for (const item of departments) {
      const node = nodes.get(item.id)!
      const parent = item.parentId ? nodes.get(item.parentId) : undefined
      if (parent && parent !== node) parent.children!.push(node)
      else roots.push(node)
    }
    const term = search.trim().toLocaleLowerCase('zh-CN')
    if (!term) return roots
    const filter = (node: TreeNode): TreeNode | null => {
      const item = byID.get(node.key)!
      const children = node.children?.map(filter).filter((child): child is TreeNode => child !== null) ?? []
      if (!item.name.toLocaleLowerCase('zh-CN').includes(term) && children.length === 0) return null
      return { ...node, children }
    }
    return roots.map(filter).filter((node): node is TreeNode => node !== null)
  }, [departments, byID, search])

  const toggle = (id: string, checked: boolean) => {
    const next = new Set(selectedIds)
    const pending = [id]
    const visited = new Set<string>()
    while (pending.length > 0) {
      const current = pending.pop()!
      if (visited.has(current)) continue
      visited.add(current)
      if (checked && byID.get(current)?.status === 'active') next.add(current)
      if (!checked) next.delete(current)
      pending.push(...(childrenByParent.get(current) ?? []))
    }
    onChange([...next])
  }

  return <div className="policy-department-picker">
    <Input.Search value={search} onChange={(event) => setSearch(event.target.value)} allowClear placeholder="搜索部门名称" />
    <div className="policy-department-tree">
      <Tree key={search ? `search:${search}` : 'all'} treeData={treeData} checkable checkStrictly
        checkedKeys={selectedIds} selectable={false} height={320}
        defaultExpandAll={Boolean(search)}
        onCheck={(_, info) => toggle(String(info.node.key), info.checked)} />
    </div>
    <Text type="secondary">已勾选 {selectedIds.length} 个部门；只对这些部门分别保存策略，新建的下级部门不会自动加入。</Text>
  </div>
}
