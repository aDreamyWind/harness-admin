import { Upload } from 'antd'
import { useEffect, useState } from 'react'

const maxCoverBytes = 10 * 1024 * 1024
export const coverFileTypes = 'image/jpeg,image/png,image/webp'

export function useCoverUpload() {
  const [picked, setPicked] = useState<{ file: File; url: string }>()
  const [fileError, setFileError] = useState('')

  useEffect(() => () => { if (picked) URL.revokeObjectURL(picked.url) }, [picked])

  function pickFile(file: File) {
    setPicked(undefined)
    if (file.size === 0 || file.size > maxCoverBytes) {
      setFileError('请选择不超过 10 MiB 的图片。')
    } else if (file.type && !coverFileTypes.split(',').includes(file.type)) {
      setFileError('请选择 JPG、PNG 或 WebP 图片。')
    } else {
      setFileError('')
      setPicked({ file, url: URL.createObjectURL(file) })
    }
    return Upload.LIST_IGNORE
  }

  function clearFile() {
    setPicked(undefined)
    setFileError('')
  }

  return { picked, fileError, pickFile, clearFile }
}
