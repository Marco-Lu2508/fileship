import { locale, translateError } from '../stores/i18n.js'
import { get } from 'svelte/store'

export function formatBytes(bytes = 0, language = get(locale)) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1
  )
  return `${new Intl.NumberFormat(language, { maximumFractionDigits: index > 0 ? 1 : 0 }).format(bytes / 1024 ** index)} ${units[index]}`
}

export function fileKind(file) {
  if (file.is_dir) return 'folder'
  const mime = file.mime_type || ''
  const name = file.name || ''
  if (
    mime.startsWith('image/') ||
    /\.(png|jpe?g|gif|webp|bmp|svg)$/i.test(name)
  )
    return 'image'
  if (
    mime.startsWith('video/') ||
    /\.(mp4|webm|ogv|mov|m4v|mkv|avi|flv|wmv|3gp|mts|m2ts)$/i.test(name)
  )
    return 'video'
  if (mime.startsWith('audio/') || /\.(mp3|wav|ogg|oga|m4a|flac)$/i.test(name))
    return 'audio'
  if (/\.(zip|gz|tar|7z|rar)$/i.test(name)) return 'archive'
  if (
    mime.includes('pdf') ||
    mime.startsWith('text/') ||
    /\.(pdf|md|txt|docx?|xlsx?|pptx?|json|yaml|yml|csv|log|xml|html|css|js|ts|go|py|sh)$/i.test(
      name
    )
  )
    return 'text'
  return 'file'
}

export function validName(name) {
  return (
    !!name.trim() &&
    !/[\\/\x00-\x1f]/.test(name) &&
    !['.', '..'].includes(name.trim())
  )
}

export async function requireOK(
  response,
  fallback = 'The action could not be completed'
) {
  if (response.ok) return response
  const message = (await response.text()).trim()
  throw new Error(
    translateError(
      message && message.length < 240 && !message.startsWith('<')
        ? message
        : fallback
    )
  )
}

export async function saveDownload(response, name) {
  await requireOK(response, 'Download failed')
  const url = URL.createObjectURL(await response.blob())
  const link = document.createElement('a')
  link.href = url
  link.download = name
  document.body.append(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
