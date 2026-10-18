import { test, afterEach } from 'node:test'
import assert from 'node:assert/strict'
import { compileSvelte, setupDOM } from './helpers/components.js'

setupDOM()
const { Preview, Editor, mount, unmount, flushSync, locale, auth, toast } =
  await compileSvelte(`
    export { default as Preview } from './src/components/Preview.svelte'
    export { default as Editor } from './src/components/Editor.svelte'
    export { locale } from './src/stores/i18n.js'
    export * as auth from './src/stores/auth.js'
    export * as toast from './src/stores/toast.js'
  `)

const instances = []
function render(Component, props) {
  const target = document.createElement('div')
  document.body.append(target)
  const instance = flushSync(() => mount(Component, { target, props }))
  instances.push({ instance, target })
  return { instance, target }
}
async function settle(predicate) {
  for (let i = 0; i < 200; i++) {
    await new Promise((resolve) => setTimeout(resolve, 10))
    flushSync()
    if (predicate) {
      if (predicate()) return
    } else if (i >= 3) {
      return
    }
  }
  if (predicate && !predicate()) {
    assert.fail('component did not reach the expected state')
  }
}
const file = (path, mime_type = 'text/plain') => ({
  path,
  name: path.split('/').pop(),
  mime_type
})
const json = (body) =>
  new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' }
  })
function change(target, value) {
  const textarea = target.querySelector('textarea')
  textarea.value = value
  textarea.dispatchEvent(new Event('input', { bubbles: true }))
  flushSync()
}
function unload() {
  const event = new Event('beforeunload', { cancelable: true })
  window.dispatchEvent(event)
  return event.defaultPrevented
}

afterEach(async () => {
  for (const { instance, target } of instances.splice(0)) {
    await unmount(instance)
    target.remove()
  }
  locale.set('en')
  auth.accessToken.set('test-access')
  toast.toasts.set([])
})

test('preview ignores text decoding that finishes after navigating to another file', async () => {
  let release
  let firstSignal
  globalThis.fetch = async (url, options) => {
    if (String(url).includes('first.txt')) {
      firstSignal = options.signal
      return {
        ok: true,
        text: () =>
          new Promise((resolve) => {
            release = resolve
          })
      }
    }
    return new Response('Second file content')
  }
  const { instance, target } = render(Preview, { file: file('first.txt') })
  await settle()
  flushSync(() => {
    instance.file = file('second.txt')
  })
  await settle(() => target.querySelector('pre'))
  assert.equal(firstSignal.aborted, true)
  assert.equal(target.querySelector('pre').textContent, 'Second file content')
  release('Late first file content')
  await settle(() => target.querySelector('pre'))
  assert.equal(target.querySelector('pre').textContent, 'Second file content')
  assert.equal(
    new URL(target.querySelector('a[download]').href).searchParams.get('path'),
    'second.txt'
  )
})

test('closing a preview aborts pending text and does not leak it into a reopened file', async () => {
  let release
  let signal
  globalThis.fetch = async (url, options) => {
    if (String(url).includes('pending.txt')) {
      signal = options.signal
      return new Promise((resolve) => {
        release = resolve
      })
    }
    return new Response('New text')
  }
  const { instance, target } = render(Preview, { file: file('pending.txt') })
  await settle()
  flushSync(() => {
    instance.file = null
  })
  assert.equal(signal.aborted, true)
  flushSync(() => {
    instance.file = file('reopened.txt')
  })
  await settle(() => target.querySelector('pre'))
  release(new Response('Old text'))
  await settle(() => target.querySelector('pre'))
  assert.equal(target.querySelector('pre').textContent, 'New text')
})

test('media navigation ignores old image errors and downloads use the attachment endpoint', async () => {
  globalThis.fetch = async () => {
    throw new Error('media should stream directly')
  }
  const { instance, target } = render(Preview, {
    file: file('first.png', 'image/png')
  })
  const previous = target.querySelector('img')
  flushSync(() => {
    instance.file = file('photos/second #.png', 'image/png')
  })
  previous.dispatchEvent(new Event('error'))
  flushSync()
  assert.equal(target.querySelector('img').alt, 'second #.png')
  const download = new URL(target.querySelector('a[download]').href)
  assert.equal(download.pathname, '/api/files/download')
  assert.equal(download.searchParams.get('path'), 'photos/second #.png')
  auth.accessToken.set('renewed-access')
  flushSync()
  assert.equal(
    new URL(target.querySelector('a[download]').href).searchParams.get(
      'access_token'
    ),
    'renewed-access'
  )
})

test('video preview retries with live transcode=1 stream before showing error state', async () => {
  const { target } = render(Preview, {
    file: file('clips/camera.mkv', 'video/x-matroska')
  })
  const video = target.querySelector('video')
  assert.ok(video)
  assert.equal(new URL(video.src).searchParams.get('transcode'), null)
  video.dispatchEvent(new Event('error'))
  flushSync()
  const retriedVideo = target.querySelector('video')
  assert.ok(retriedVideo)
  assert.equal(new URL(retriedVideo.src).searchParams.get('transcode'), '1')
  retriedVideo.dispatchEvent(new Event('error'))
  flushSync()
  assert.equal(target.querySelector('video'), null)
  assert.match(target.textContent, /Preview could not be loaded/)
})

test('translating and refreshing file metadata preserve a loaded preview', async () => {
  let loads = 0
  globalThis.fetch = async () => {
    loads++
    return new Response('Persistent text')
  }
  const { instance, target } = render(Preview, { file: file('stable.txt') })
  await settle()
  locale.set('de')
  flushSync(() => {
    instance.file = { ...file('stable.txt'), size: 100 }
  })
  await settle(() => target.querySelector('pre'))
  assert.equal(loads, 1)
  assert.equal(target.querySelector('pre').textContent, 'Persistent text')
})

test('destroying a preview cancels its pending request', async () => {
  let signal
  globalThis.fetch = async (_url, options) => {
    signal = options.signal
    return new Promise(() => {})
  }
  const { instance, target } = render(Preview, { file: file('pending.txt') })
  await settle()
  await unmount(instance)
  instances.splice(0)
  target.remove()
  assert.equal(signal.aborted, true)
})

test('editor preserves unsaved edits on metadata refresh and guards close and browser exit', async () => {
  let loads = 0
  let closes = 0
  let confirmations = 0
  globalThis.confirm = () => {
    confirmations++
    return false
  }
  globalThis.fetch = async () => {
    loads++
    return json({ content: 'Original' })
  }
  const { instance, target } = render(Editor, {
    file: file('notes.txt'),
    onClose: () => {
      closes++
    }
  })
  await settle()
  change(target, 'Unsaved work')
  flushSync(() => {
    instance.file = { ...file('notes.txt'), size: 12 }
  })
  await settle()
  assert.equal(loads, 1)
  assert.equal(target.querySelector('textarea').value, 'Unsaved work')
  target.querySelector('button.close').click()
  assert.equal(confirmations, 1)
  assert.equal(closes, 0)
  assert.equal(unload(), true)
  globalThis.confirm = () => true
  target.querySelector('button.close').click()
  assert.equal(closes, 1)
})

test('typing during save remains dirty after the saved snapshot completes', async () => {
  let release
  let saved
  globalThis.fetch = async (_url, options) => {
    if (options.method === 'PUT') {
      saved = JSON.parse(options.body)
      return new Promise((resolve) => {
        release = resolve
      })
    }
    return json({ content: 'Original' })
  }
  const { target } = render(Editor, { file: file('notes.txt') })
  await settle()
  change(target, 'First edit')
  target.querySelector('.save-btn').click()
  await settle()
  change(target, 'Second edit')
  release(new Response(null, { status: 204 }))
  await settle()
  assert.deepEqual(saved, { path: 'notes.txt', content: 'First edit' })
  assert.equal(target.querySelector('textarea').value, 'Second edit')
  assert.equal(target.querySelector('.save-btn').disabled, false)
  assert.equal(unload(), true)
})

test('pending saves guard browser exit even if the editor is reverted to the original text', async () => {
  let release
  globalThis.fetch = async (_url, options) =>
    options.method === 'PUT'
      ? new Promise((resolve) => {
          release = resolve
        })
      : json({ content: 'Original' })
  const { target } = render(Editor, { file: file('notes.txt') })
  await settle()
  change(target, 'Saving edit')
  window.dispatchEvent(
    new KeyboardEvent('keydown', { key: 'S', ctrlKey: true, cancelable: true })
  )
  await settle()
  change(target, 'Original')
  assert.equal(unload(), true)
  release(new Response(null, { status: 204 }))
  await settle()
  assert.equal(unload(), true)
})

test('editor aborts loading on destroy and ignores a subsequent load failure', async () => {
  let reject
  let signal
  let closes = 0
  globalThis.fetch = async (_url, options) => {
    signal = options.signal
    return new Promise((_resolve, rejectLoad) => {
      reject = rejectLoad
    })
  }
  const { instance, target } = render(Editor, {
    file: file('notes.txt'),
    onClose: () => {
      closes++
    }
  })
  await settle()
  await unmount(instance)
  instances.splice(0)
  target.remove()
  assert.equal(signal.aborted, true)
  reject(new Error('late failure'))
  await settle()
  assert.equal(closes, 0)
  let toasts
  const unsubscribe = toast.toasts.subscribe((items) => {
    toasts = items
  })
  assert.deepEqual(toasts, [])
  unsubscribe()
})
