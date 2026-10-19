import { Select, type SelectProps } from 'antd'
import { useMemo } from 'react'

import type { Department } from '../api/departments'

interface DepartmentSelectProps extends Omit<SelectProps<string>, 'options' | 'optionRender'> {
  departments: Department[]
  excludeDepartmentId?: string
}

export function DepartmentSelect({ departments, excludeDepartmentId, ...props }: DepartmentSelectProps) {
  const options = useMemo(() => {
    const excluded = collectDepartmentAndDescendants(departments, excludeDepartmentId)
    const paths = buildDepartmentPaths(departments)
    return departments
      .filter((department) => (department.status === 'active' || department.id === props.value) && !excluded.has(department.id))
      .map((department) => ({
        value: department.id,
        label: department.name,
        pathLabel: paths.get(department.id) ?? department.name,
        searchText: `${department.name} ${paths.get(department.id) ?? ''}`.toLocaleLowerCase('zh-CN'),
      }))
  }, [departments, excludeDepartmentId, props.value])

  return (
    <Select
      {...props}
      allowClear
      showSearch
      options={options}
      filterOption={(input, option) => String(option?.searchText ?? '').includes(input.trim().toLocaleLowerCase('zh-CN'))}
      optionRender={(option) => (
        <div className="department-option">
          <span>{option.label}</span>
          {option.data.pathLabel !== option.label ? <small>（{String(option.data.pathLabel)}）</small> : null}
        </div>
      )}
    />
  )
}

function buildDepartmentPaths(departments: Department[]): Map<string, string> {
  const byID = new Map(departments.map((department) => [department.id, department]))
  const paths = new Map<string, string>()
  const resolve = (department: Department, visited = new Set<string>()): string => {
    const cached = paths.get(department.id)
    if (cached) return cached
    if (!department.parentId || visited.has(department.id)) {
      paths.set(department.id, department.name)
      return department.name
    }
    visited.add(department.id)
    const parent = byID.get(department.parentId)
    const path = parent ? `${resolve(parent, visited)} - ${department.name}` : department.name
    paths.set(department.id, path)
    return path
  }
  departments.forEach((department) => resolve(department))
  return paths
}

function collectDepartmentAndDescendants(departments: Department[], rootID?: string): Set<string> {
  const excluded = new Set<string>()
  if (!rootID) return excluded
  const children = new Map<string, string[]>()
  departments.forEach((department) => {
    if (!department.parentId) return
    children.set(department.parentId, [...(children.get(department.parentId) ?? []), department.id])
  })
  const visit = (id: string) => {
    excluded.add(id)
    ;(children.get(id) ?? []).forEach(visit)
  }
  visit(rootID)
  return excluded
}
