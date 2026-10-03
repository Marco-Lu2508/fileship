import { apiFetch } from '../stores/auth.js'
import { requireOK, validName } from './files.js'
import { t, translateError } from '../stores/i18n.js'

export function topLevelPaths(paths) {
  return [...new Set(paths)].filter(
    (path) =>
      !paths.some((other) => other !== path && path.startsWith(other + '/'))
  )
}

export function destinationPath(
  source,
  folder,
  name = source.split('/').pop()
) {
  if (!validName(name))
    throw new Error(t('Use a name without slashes or control characters'))
  const normalized = folder.replace(/^\/+|\/+$/g, '')
  if (normalized.split('/').includes('..') || normalized.includes('\\'))
    throw new Error(t('Invalid destination folder'))
  return normalized ? `${normalized}/${name}` : name
}

export async function transferFiles(paths, folder, mode, name = '') {
  if (!['copy', 'move'].includes(mode)) throw new Error(t('Invalid operation'))
  const results = []
  for (const src of topLevelPaths(paths)) {
    try {
      const dst = destinationPath(
        src,
        folder,
        paths.length === 1 && name ? name : undefined
      )
      if (dst === src || dst.startsWith(src + '/'))
        throw new Error(t('Choose a different destination'))
      await requireOK(
        await apiFetch(`/api/files/${mode}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ src, dst })
        })
      )
      results.push({ path: src, ok: true })
    } catch (error) {
      results.push({
        path: src,
        ok: false,
        error: translateError(error.message)
      })
    }
  }
  return results
}
