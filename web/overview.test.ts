import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { formatAgo, formatUptime, hostsSummary, warningText } from './src/overview.ts'
import type { HostWarning } from './src/types.ts'

describe('formatUptime', () => {
  it('formats seconds, minutes, hours, days', () => {
    assert.equal(formatUptime(0), '刚刚')
    assert.equal(formatUptime(59), '59 秒')
    assert.equal(formatUptime(90), '1 分钟')
    assert.equal(formatUptime(3700), '1 小时 1 分钟')
    assert.equal(formatUptime(90000), '1 天 1 小时')
  })

  it('omits zero minutes and hours', () => {
    assert.equal(formatUptime(60), '1 分钟')
    assert.equal(formatUptime(3600), '1 小时')
    assert.equal(formatUptime(86400), '1 天')
    assert.equal(formatUptime(86400 + 3600 + 60), '1 天 1 小时')
  })
})

describe('formatAgo', () => {
  const now = Date.parse('2026-11-22T00:00:00Z')
  it('formats relative times', () => {
    assert.equal(formatAgo('2026-11-21T23:59:58Z', now), '2 秒前')
    assert.equal(formatAgo('2026-11-21T23:00:00Z', now), '1 小时前')
    assert.equal(formatAgo('bad', now), '—')
  })

  it('covers just-now, minutes and days', () => {
    assert.equal(formatAgo('2026-11-22T00:00:00Z', now), '刚刚')
    assert.equal(formatAgo('2026-11-21T23:59:30Z', now), '30 秒前')
    assert.equal(formatAgo('2026-11-21T23:58:00Z', now), '2 分钟前')
    assert.equal(formatAgo('2026-11-20T00:00:00Z', now), '2 天前')
  })
})

describe('hostsSummary', () => {
  it('lists three and counts the rest', () => {
    assert.deepEqual(hostsSummary(['a', 'b', 'c', 'd']), { text: 'a、b、c 等 4 个', title: 'a、b、c、d' })
    assert.deepEqual(hostsSummary([]), { text: '—', title: '' })
  })

  it('does not count the rest at the limit and honours max', () => {
    assert.deepEqual(hostsSummary(['a', 'b', 'c']), { text: 'a、b、c', title: 'a、b、c' })
    assert.deepEqual(hostsSummary(['a', 'b', 'c'], 2), { text: 'a、b 等 3 个', title: 'a、b、c' })
  })
})

describe('warningText', () => {
  it('names both lines', () => {
    const w: HostWarning = { domain: 'a', ignored_line: 5, first_line: 3 }
    assert.equal(warningText(w), '第 5 行与第 3 行重复，已按第 3 行生效')
  })
})
