import { test, beforeEach, afterEach } from 'node:test'
import assert from 'node:assert/strict'
import { setupDOM, compileSvelte } from './helpers/components.js'

setupDOM()
globalThis.location = window.location
globalThis.WebSocket = class {
  close() {}
}
const ui = await compileSvelte(`
  export { default as Browser } from './src/components/Browser.svelte';
  export { default as Dropzone } from './src/components/Dropzone.svelte';
  export * as workspace from './src/stores/files.js';
  export { locale } from './src/stores/i18n.js';
`)
const json = (value) =>
  new Response(JSON.stringify(value), {
    headers: { 'Content-Type': 'application/json' }
  })
const file = (path, is_dir = false) => ({
  path,
  name: path.split('/').pop(),
  is_dir,
  size: 10,
  mod_time: '2026-10-17T12:00:00Z',
  mime_type: is_dir ? '' : 'text/plain'
})
let component, target, requests, folderFiles, searchFiles
async function settle(predicate) {
  for (let i = 0; i < 100; i++) {
    await new Promise((resolve) => setTimeout(resolve, 10))
    ui.flushSync()
    if (predicate()) return
  }
  assert.fail('component did not reach the expected state')
}
function mount(Component = ui.Browser) {
  ui.flushSync(() => {
    component = ui.mount(Component, { target })
  })
}
function search(value) {
  const input = target.querySelector('input[aria-label="Search files"]')
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  ui.flushSync()
}

beforeEach(() => {
  target = document.createElement('div')
  document.body.append(target)
  requests = []
  folderFiles = [file('current.txt')]
  searchFiles = [file('docs/report.txt')]
  ui.locale.set('en')
  globalThis.fetch = async (url, options = {}) => {
    requests.push({ url: String(url), options })
    if (String(url).startsWith('/api/files/search')) return json(searchFiles)
    if (String(url).startsWith('/api/files?') && options.method === 'DELETE') {
      searchFiles = []
      return new Response(null, { status: 204 })
    }
    if (String(url).startsWith('/api/files?'))
      return json({
        files: folderFiles,
        total: folderFiles.length,
        page: 1,
        per_page: 100
      })
    if (url === '/api/me/favorites') return json([])
    if (url === '/api/me/settings') return json({ disk_usage: 10 })
    return new Response(null, { status: 204 })
  }
})
afterEach(async () => {
  if (component) await ui.unmount(component)
  component = null
  target.remove()
})

test('select all during a pending search never selects hidden folder files', async () => {
  mount()
  await settle(() => target.textContent.includes('current.txt'))
  search('report')
  document.body.dispatchEvent(
    new KeyboardEvent('keydown', {
      key: 'a',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
  )
  let selection
  const stop = ui.workspace.selected.subscribe((value) => (selection = value))
  assert.equal(selection.size, 0)
  stop()
  await settle(() => target.textContent.includes('report.txt'))
  assert.equal(target.textContent.includes('current.txt'), false)
})

test('search sorting stays local and clearing search applies the chosen folder order', async () => {
  mount()
  await settle(() => target.textContent.includes('current.txt'))
  search('report')
  await settle(() => target.textContent.includes('report.txt'))
  const folderRequests = () =>
    requests.filter((r) => r.url.startsWith('/api/files?'))
  const before = folderRequests().length
  const sort = target.querySelector('select[aria-label="Sort files"]')
  sort.value = 'size:false'
  sort.dispatchEvent(new Event('change', { bubbles: true }))
  ui.flushSync()
  assert.equal(folderRequests().length, before)
  ui.workspace.loadError.set('folder temporarily unavailable')
  ui.flushSync()
  assert.ok(target.textContent.includes('report.txt'))
  assert.equal(target.textContent.includes('Something didn’t load'), false)
  target.querySelector('button[aria-label="Clear search"]').click()
  await settle(() => target.textContent.includes('current.txt'))
  const last = new URL(folderRequests().at(-1).url, 'http://localhost')
  assert.equal(last.searchParams.get('sort_by'), 'size')
  assert.equal(last.searchParams.get('sort_asc'), 'false')
})

test('deleting a folder and its search descendants sends one deletion and refreshes search', async () => {
  searchFiles = [file('docs', true), file('docs/report.txt')]
  mount()
  await settle(() => target.textContent.includes('current.txt'))
  search('docs')
  await settle(() => target.textContent.includes('report.txt'))
  ui.workspace.selected.set(new Set(['docs', 'docs/report.txt']))
  ui.flushSync()
  document.body.dispatchEvent(
    new KeyboardEvent('keydown', {
      key: 'Delete',
      bubbles: true
    })
  )
  ui.flushSync()
  const dialog = target.querySelector('dialog')
  assert.equal(dialog.querySelector('h2').textContent, 'Move 1 items to trash?')
  const confirm = [...dialog.querySelectorAll('button')].find((button) =>
    button.textContent.includes('Move to trash')
  )
  confirm.click()
  await settle(() => target.textContent.includes('No files found'))
  const deletions = requests.filter((r) => r.options.method === 'DELETE')
  assert.equal(deletions.length, 1)
  assert.equal(
    new URL(deletions[0].url, 'http://localhost').searchParams.get('path'),
    'docs'
  )
})

test('the German upload area accepts the browser Files drag type exactly once', async () => {
  ui.locale.set('de')
  let uploads = 0
  globalThis.XMLHttpRequest = class {
    upload = {}
    open() {}
    setRequestHeader() {}
    send() {
      uploads++
      this.status = 201
      this.responseText = ''
      queueMicrotask(() => this.onload())
    }
    abort() {
      this.onabort?.()
    }
  }
  mount(ui.Dropzone)
  const region = target.querySelector('[aria-label="Upload-Bereich"]')
  assert.ok(region)
  const dataTransfer = {
    types: ['Files'],
    files: [new File(['hello'], 'hello.txt')]
  }
  for (const type of ['dragover', 'drop']) {
    const event = new Event(type, { bubbles: true, cancelable: true })
    Object.defineProperty(event, 'dataTransfer', { value: dataTransfer })
    region.dispatchEvent(event)
    assert.equal(event.defaultPrevented, true)
  }
  await settle(
    () => uploads === 1 && target.textContent.includes('Abgeschlossen')
  )
  assert.equal(uploads, 1)
})
