import { marked } from 'marked'
import DOMPurify from 'dompurify'

export function renderMarkdown(source, purifier = DOMPurify) {
  return purifier.sanitize(marked.parse(source, { async: false }), {
    USE_PROFILES: { html: true },
    FORBID_TAGS: ['img', 'iframe', 'form', 'style', 'video', 'audio'],
    FORBID_ATTR: ['style', 'src', 'srcset']
  })
}
