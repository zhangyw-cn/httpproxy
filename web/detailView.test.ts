import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  DOCK_STORAGE_KEY,
  displayBody,
  headersCopyText,
  loadDock,
  normalizeDock,
  queryParams,
  saveDock,
} from './src/detailView.ts'

describe('queryParams', () => {
  it('parses the spec example', () => {
    assert.deepEqual(queryParams('/search?q=a+b&x=%20&novalue&a=1&'), [
      { name: 'q', value: 'a b' },
      { name: 'x', value: ' ' },
      { name: 'novalue', value: '' },
      { name: 'a', value: '1' },
    ])
  })

  it('returns nothing when there is no query', () => {
    assert.deepEqual(queryParams('/search'), [])
    assert.deepEqual(queryParams('/search?'), [])
  })

  it('keeps a side that fails to decode', () => {
    assert.deepEqual(queryParams('/?bad=%ZZ&ok=1'), [
      { name: 'bad', value: '%ZZ' },
      { name: 'ok', value: '1' },
    ])
  })
})

describe('headersCopyText', () => {
  it('matches the spec example', () => {
    assert.equal(
      headersCopyText({
        url: 'https://example.com/a?q=1',
        method: 'GET',
        status: '200',
        version: 'HTTP/1.1',
        client: '127.0.0.1:54321',
        query: [{ name: 'q', value: '1' }],
        respHeaders: [{ name: 'Content-Type', value: 'text/plain' }],
        reqHeaders: [{ name: 'Host', value: 'example.com' }],
      }),
      [
        '常规',
        '请求 URL：https://example.com/a?q=1',
        '请求方法：GET',
        '状态码：200',
        '协议：HTTP/1.1',
        '远程地址：127.0.0.1:54321',
        '',
        '查询参数',
        'q: 1',
        '',
        '响应标头 (1)',
        'Content-Type: text/plain',
        '',
        '请求标头 (1)',
        'Host: example.com',
      ].join('\n'),
    )
  })

  it('adds an error, an empty query, and zero counts for null headers', () => {
    assert.equal(
      headersCopyText({
        url: 'https://example.com/a',
        method: 'GET',
        status: '(失败)',
        version: 'HTTP/1.1',
        client: '127.0.0.1:1',
        error: 'boom',
        query: [],
        respHeaders: null,
        reqHeaders: null,
      }),
      [
        '常规',
        '请求 URL：https://example.com/a',
        '请求方法：GET',
        '状态码：(失败)',
        '协议：HTTP/1.1',
        '远程地址：127.0.0.1:1',
        '错误：boom',
        '',
        '查询参数',
        '(无)',
        '',
        '响应标头 (0)',
        '',
        '请求标头 (0)',
      ].join('\n'),
    )
  })

  it('writes an empty value with nothing after the colon', () => {
    const text = headersCopyText({
      url: 'https://example.com/',
      method: 'GET',
      status: '204',
      version: 'HTTP/1.1',
      client: '127.0.0.1:1',
      query: [{ name: 'novalue', value: '' }],
      respHeaders: [],
      reqHeaders: [],
    })
    assert.equal(text.includes('novalue:'), true)
    assert.equal(text.includes('novalue: '), false)
  })
})

describe('displayBody', () => {
  it('indents JSON when pretty and keeps the original when raw', () => {
    const headers = [{ name: 'Content-Type', value: 'text/plain' }]
    assert.equal(displayBody('{"a":1}', headers, 'pretty'), '{\n  "a": 1\n}')
    assert.equal(displayBody('{"a":1}', headers, 'raw'), '{"a":1}')
  })
})

describe('dock storage', () => {
  it('accepts only bottom and right', () => {
    assert.equal(normalizeDock('bottom'), 'bottom')
    assert.equal(normalizeDock('right'), 'right')
    assert.equal(normalizeDock('left'), 'bottom')
    assert.equal(normalizeDock(null), 'bottom')
    assert.equal(normalizeDock(1), 'bottom')
  })

  it('round-trips through injected storage and rejects broken JSON', () => {
    const data = new Map<string, string>()
    const store = {
      getItem(key: string) {
        return data.has(key) ? data.get(key)! : null
      },
      setItem(key: string, value: string) {
        data.set(key, value)
      },
    }
    assert.equal(loadDock(store), 'bottom')
    saveDock('right', store)
    assert.equal(data.get(DOCK_STORAGE_KEY), '"right"')
    assert.equal(loadDock(store), 'right')
    data.set(DOCK_STORAGE_KEY, '{')
    assert.equal(loadDock(store), 'bottom')
  })
})
