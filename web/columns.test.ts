import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  COLUMNS,
  COLUMN_STORAGE_KEY,
  DEFAULT_VISIBLE,
  activeColumns,
  flowCell,
  formatStarted,
  loadVisible,
  normalizeVisible,
  saveVisible,
  tableMinWidth,
  type CellFlow,
} from './src/columns.ts'

const format = {
  size: (n: number) => `S${n}`,
  time: (ms: number) => `T${ms}`,
}

function flow(patch: Partial<CellFlow> = {}): CellFlow {
  return {
    scheme: 'https',
    method: 'GET',
    host: 'example.test',
    path: '/a',
    http_version: '2',
    status_code: 200,
    req_size: 12,
    resp_size: 34,
    started_at: '2026-10-22T01:02:03.006Z',
    duration_ms: 1500,
    client_addr: '10.0.0.8:4000',
    ...patch,
  }
}

function memory(initial?: string) {
  const data = new Map<string, string>()
  if (initial !== undefined) data.set(COLUMN_STORAGE_KEY, initial)
  return {
    getItem(key: string) {
      return data.has(key) ? data.get(key)! : null
    },
    setItem(key: string, value: string) {
      data.set(key, value)
    },
    raw: data,
  }
}

describe('catalog', () => {
  it('lists the spec columns in order', () => {
    assert.deepEqual(
      COLUMNS.map((c) => [c.id, c.label, c.locked, c.width, c.align, c.defaultVisible]),
      [
        ['name', '名称', true, 'auto', 'left', true],
        ['host', '主机', false, 180, 'left', true],
        ['status', '状态', false, 80, 'left', true],
        ['method', '方法', false, 72, 'left', true],
        ['version', '版本', false, 72, 'left', true],
        ['size', '大小', false, 80, 'right', true],
        ['reqSize', '请求大小', false, 88, 'right', false],
        ['time', '耗时', false, 80, 'right', true],
        ['started', '开始时间', false, 112, 'left', false],
        ['client', '客户端地址', false, 148, 'left', false],
        ['error', '错误', false, 200, 'left', false],
      ],
    )
    assert.deepEqual(DEFAULT_VISIBLE, ['host', 'status', 'method', 'version', 'size', 'time'])
    assert.equal(COLUMN_STORAGE_KEY, 'httpproxy.columns')
  })
})

describe('normalizeVisible', () => {
  it('restores the default for non-arrays', () => {
    for (const raw of [null, undefined, 'host', 1, { host: true }]) {
      assert.deepEqual(normalizeVisible(raw), DEFAULT_VISIBLE)
    }
  })

  it('drops unknown ids, name, non-strings, and duplicates, then sorts', () => {
    assert.deepEqual(normalizeVisible(['time', 'host', 'host', 'name', 'nope', 1, null]), ['host', 'time'])
    assert.deepEqual(normalizeVisible([]), [])
    assert.deepEqual(normalizeVisible(['nope']), [])
  })
})

describe('storage', () => {
  it('uses the default when the key is missing and does not write', () => {
    const store = memory()
    assert.deepEqual(loadVisible(store), DEFAULT_VISIBLE)
    assert.equal(store.raw.size, 0)
  })

  it('uses the default for illegal JSON and non-arrays', () => {
    assert.deepEqual(loadVisible(memory('{')), DEFAULT_VISIBLE)
    assert.deepEqual(loadVisible(memory('{"host":true}')), DEFAULT_VISIBLE)
    assert.deepEqual(loadVisible(memory('null')), DEFAULT_VISIBLE)
  })

  it('keeps a legal array after filtering', () => {
    assert.deepEqual(loadVisible(memory('["host","nope"]')), ['host'])
    assert.deepEqual(loadVisible(memory('[]')), [])
  })

  it('returns the default when reading storage throws', () => {
    assert.deepEqual(
      loadVisible({
        getItem() {
          throw new Error('denied')
        },
      }),
      DEFAULT_VISIBLE,
    )
    assert.deepEqual(loadVisible(), DEFAULT_VISIBLE)
  })

  it('writes canonical JSON and swallows write failures', () => {
    const store = memory()
    saveVisible(['time', 'host', 'host', 'nope'], store)
    assert.equal(store.raw.get(COLUMN_STORAGE_KEY), '["host","time"]')
    assert.doesNotThrow(() =>
      saveVisible(['host'], {
        setItem() {
          throw new Error('denied')
        },
      }),
    )
  })
})

describe('formatStarted', () => {
  it('formats local time with zero-padded milliseconds', () => {
    const local = new Date(2026, 9, 22, 9, 8, 7, 6)
    assert.equal(formatStarted(local.toISOString()), '09:08:07.006')
    const raw = '2026-10-22T01:02:03.006789123+08:00'
    const d = new Date(raw)
    const p = (n: number, w = 2) => String(n).padStart(w, '0')
    assert.equal(
      formatStarted(raw),
      `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`,
    )
  })

  it('returns the raw string when it is not a time', () => {
    assert.equal(formatStarted('not-a-date'), 'not-a-date')
    assert.equal(formatStarted(''), '')
  })
})

describe('flowCell', () => {
  it('shows only the path in the name cell and puts the full URL on the title', () => {
    assert.deepEqual(flowCell('name', flow(), format), {
      text: '/a',
      title: 'https://example.test/a',
    })
    assert.deepEqual(flowCell('name', flow({ path: '' }), format), {
      text: '/',
      title: 'https://example.test/',
    })
  })

  it('titles host, client, and error only when they are non-empty', () => {
    assert.deepEqual(flowCell('host', flow(), format), { text: 'example.test', title: 'example.test' })
    assert.deepEqual(flowCell('host', flow({ host: '' }), format), { text: '' })
    assert.deepEqual(flowCell('client', flow(), format), { text: '10.0.0.8:4000', title: '10.0.0.8:4000' })
    assert.deepEqual(flowCell('error', flow({ error: 'boom' }), format), { text: 'boom', title: 'boom' })
    assert.deepEqual(flowCell('error', flow(), format), { text: '' })
  })

  it('keeps the existing status words and classes', () => {
    assert.deepEqual(flowCell('status', flow({ error: 'boom', status_code: 500 }), format), {
      text: '(失败)',
      statusClass: 'st-err',
    })
    assert.deepEqual(flowCell('status', flow({ status_code: 0 }), format), {
      text: '(等待)',
      statusClass: 'st-pending',
    })
    assert.deepEqual(flowCell('status', flow({ status_code: 200 }), format), {
      text: '200',
      statusClass: 'st-ok',
    })
    assert.deepEqual(flowCell('status', flow({ status_code: 302 }), format), {
      text: '302',
      statusClass: 'st-redirect',
    })
    assert.deepEqual(flowCell('status', flow({ status_code: 404 }), format), {
      text: '404',
      statusClass: 'st-err',
    })
  })

  it('formats method, version, both sizes, duration, and start time', () => {
    assert.deepEqual(flowCell('method', flow(), format), { text: 'GET' })
    assert.deepEqual(flowCell('version', flow(), format), { text: '2' })
    assert.deepEqual(flowCell('size', flow(), format), { text: 'S34' })
    assert.deepEqual(flowCell('reqSize', flow({ req_size: 0 }), format), { text: 'S0' })
    assert.deepEqual(flowCell('time', flow(), format), { text: 'T1500' })
    assert.deepEqual(flowCell('time', flow({ status_code: 0 }), format), { text: '…' })
    assert.equal(flowCell('time', flow({ status_code: 0, error: 'boom' }), format).text, 'T1500')
    assert.equal(flowCell('started', flow(), format).text, formatStarted('2026-10-22T01:02:03.006Z'))
  })
})

describe('layout helpers', () => {
  it('always includes name and keeps catalog order', () => {
    assert.deepEqual(
      activeColumns(['time', 'host']).map((c) => c.id),
      ['name', 'host', 'time'],
    )
    assert.deepEqual(
      activeColumns(DEFAULT_VISIBLE).map((c) => c.id),
      ['name', 'host', 'status', 'method', 'version', 'size', 'time'],
    )
    assert.deepEqual(activeColumns([]).map((c) => c.id), ['name'])
  })

  it('sums 160 for the name column plus fixed widths', () => {
    assert.equal(tableMinWidth([]), 160)
    assert.equal(tableMinWidth(DEFAULT_VISIBLE), 724)
    assert.equal(
      tableMinWidth(['host', 'status', 'method', 'version', 'size', 'reqSize', 'time', 'started', 'client', 'error']),
      1272,
    )
  })
})
