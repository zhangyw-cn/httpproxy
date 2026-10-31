import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { capturePayload, captureSummary } from './src/capture.ts'
import type { ConfigValues } from './src/types.ts'

const file: ConfigValues = {
  proxy: '127.0.0.1:8080',
  ui: '127.0.0.1:8081',
  max_flows: 1000,
  max_body: 1048576,
  theme: 'dark',
  capture: false,
  capture_empty: 'all',
  capture_https: 'decrypt',
  capture_hosts: ['example.com', '*.example.com', 'a.b.example.com', 'example.com:8443'],
}

describe('captureSummary', () => {
  it('says the config is loading', () => {
    assert.deepEqual(captureSummary(null), { text: '正在读取配置', title: '' })
  })

  it('says the config failed to load', () => {
    assert.deepEqual(captureSummary(null, true), { text: '无法读取配置', title: '' })
  })

  it('joins the paused prefix, three sections, and the total count', () => {
    const got = captureSummary(file)
    assert.equal(got.text, '抓包已暂停 · 空规则：抓全部 · 未抓 HTTPS：解密 · 主机：example.com、*.example.com、a.b.example.com 等 4 条')
    assert.equal(got.title, 'example.com\n*.example.com\na.b.example.com\nexample.com:8443')
  })

  it('drops the paused prefix and writes 无 when there are no hosts', () => {
    const got = captureSummary({ ...file, capture: true, capture_empty: 'none', capture_https: 'tunnel', capture_hosts: [] })
    assert.equal(got.text, '空规则：不抓 · 未抓 HTTPS：隧道 · 主机：无')
    assert.equal(got.title, '无')
  })
})

describe('capturePayload', () => {
  it('keeps capture and drops blank lines', () => {
    const got = capturePayload(file, 'none', 'tunnel', ' Example.com \n\n *.example.com \n')
    assert.equal(got.capture, false)
    assert.equal(got.capture_empty, 'none')
    assert.equal(got.capture_https, 'tunnel')
    assert.deepEqual(got.capture_hosts, ['Example.com', '*.example.com'])
    assert.equal(got.proxy, file.proxy)
  })
})
