import { readFile, writeFile, unlink } from 'node:fs/promises'
import { randomUUID } from 'node:crypto'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { build } from 'esbuild'
import { compile } from 'svelte/compiler'
import { JSDOM } from 'jsdom'

// Compile the actual components with the same browser runtime used by Vite.
// Entry imports are relative to frontend/, and may also export shared stores.
export async function compileSvelte(entry) {
  const result = await build({
    stdin: {
      contents: `export { mount, unmount, flushSync, tick } from 'svelte';\n${entry}`,
      resolveDir: fileURLToPath(new URL('../../', import.meta.url)),
      sourcefile: 'component-test.js'
    },
    bundle: true,
    write: false,
    format: 'esm',
    platform: 'browser',
    conditions: ['browser'],
    logLevel: 'silent',
    plugins: [
      {
        name: 'svelte-component-test',
        setup(plugin) {
          plugin.onLoad({ filter: /\.svelte$/ }, async ({ path }) => ({
            contents: compile(await readFile(path, 'utf8'), {
              filename: path,
              generate: 'client',
              accessors: true,
              css: 'injected'
            }).js.code,
            loader: 'js'
          }))
        }
      }
    ]
  })
  const bundlePath = join(
    tmpdir(),
    `fileship-component-test-${randomUUID()}.mjs`
  )
  await writeFile(bundlePath, result.outputFiles[0].text)
  try {
    return await import(pathToFileURL(bundlePath).href)
  } finally {
    await unlink(bundlePath)
  }
}

export function setupDOM() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    url: 'http://localhost',
    pretendToBeVisual: true
  })
  for (const name of [
    'window',
    'document',
    'localStorage',
    'navigator',
    'Node',
    'Element',
    'HTMLElement',
    'HTMLDialogElement',
    'HTMLInputElement',
    'HTMLTextAreaElement',
    'HTMLMediaElement',
    'HTMLSelectElement',
    'HTMLOptionElement',
    'SVGElement',
    'DocumentFragment',
    'Document',
    'Text',
    'Comment',
    'Event',
    'MouseEvent',
    'KeyboardEvent',
    'CustomEvent',
    'MutationObserver',
    'getComputedStyle',
    'requestAnimationFrame',
    'cancelAnimationFrame'
  ])
    Object.defineProperty(globalThis, name, {
      configurable: true,
      writable: true,
      value: dom.window[name]
    })
  dom.window.HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '')
  }
  dom.window.HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open')
  }
  document.cookie = 'csrf_token=component-test'
  localStorage.setItem('access_token', 'test-access')
  localStorage.setItem('locale', 'en')
  return dom
}
