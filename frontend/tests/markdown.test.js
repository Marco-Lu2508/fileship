import { test } from 'node:test'
import assert from 'node:assert/strict'
import { JSDOM } from 'jsdom'
import createDOMPurify from 'dompurify'
import { renderMarkdown } from '../src/lib/markdown.js'
test('Markdown keeps document structure but strips scripts, handlers and remote media', () => {
  const dom = new JSDOM('')
  const html = renderMarkdown(
    '# Title\n\n**Bold**\n\n<script>alert(1)</script><img src="https://example.com/track" onerror="alert(1)"><a href="javascript:alert(1)">Bad</a>',
    createDOMPurify(dom.window)
  )
  assert.match(html, /<h1>Title<\/h1>/)
  assert.match(html, /<strong>Bold<\/strong>/)
  assert.doesNotMatch(html, /<script|<img|onerror|javascript:/i)
  dom.window.close()
})
