import { useCallback, useRef, useState } from 'react'

const historyLimit = 40

// 快照只包含调用方明确提交的画布编辑。数组同步更新，连续快捷键不会读取旧记录。
export function createCanvasHistory<T>() {
  const past: T[] = []
  const future: T[] = []

  return {
    get canUndo() { return past.length > 0 },
    get canRedo() { return future.length > 0 },
    push(before: T) {
      past.push(before)
      if (past.length > historyLimit) past.shift()
      future.length = 0
    },
    undo(current: T): T | undefined {
      if (!past.length) return
      future.push(current)
      return past.pop()
    },
    redo(current: T): T | undefined {
      if (!future.length) return
      past.push(current)
      return future.pop()
    },
    clear() {
      past.length = 0
      future.length = 0
    },
  }
}

export function useCanvasHistory<T>() {
  const historyRef = useRef<ReturnType<typeof createCanvasHistory<T>> | null>(null)
  const history = historyRef.current ?? (historyRef.current = createCanvasHistory<T>())
  const [availability, setAvailability] = useState({ canUndo: false, canRedo: false })

  const syncAvailability = useCallback(() => {
    const { canUndo, canRedo } = history
    setAvailability((previous) => previous.canUndo === canUndo && previous.canRedo === canRedo ? previous : { canUndo, canRedo })
  }, [history])

  const push = useCallback((before: T) => {
    history.push(before)
    syncAvailability()
  }, [history, syncAvailability])
  const undo = useCallback((current: T) => {
    const before = history.undo(current)
    syncAvailability()
    return before
  }, [history, syncAvailability])
  const redo = useCallback((current: T) => {
    const after = history.redo(current)
    syncAvailability()
    return after
  }, [history, syncAvailability])
  const clear = useCallback(() => {
    history.clear()
    syncAvailability()
  }, [history, syncAvailability])

  return { ...availability, push, undo, redo, clear }
}
