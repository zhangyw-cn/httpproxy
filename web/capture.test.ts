import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { captureActionLabel, capturePayload, rulesMark } from './src/capture.ts'
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

describe('rulesMark', () => {
  it('says the config is loading', () => {
    assert.deepEqual(rulesMark(null), { active: false, lines: ['正在读取配置'] })
  })

  it('says the config failed to load and ignores the file', () => {
    assert.deepEqual(rulesMark(file, true), { active: false, lines: ['无法读取配置'] })
  })

  it('lists every host and stays active while capture is paused', () => {
    const got = rulesMark(file)
    assert.equal(got.active, true)
    assert.deepEqual(got.lines, [
      '空规则：抓全部',
      '未抓 HTTPS：解密',
      '主机：',
      'example.com',
      '*.example.com',
      'a.b.example.com',
      'example.com:8443',
    ])
    assert.equal(got.lines.some((line) => line.includes('抓包已暂停')), false)
    assert.equal(got.lines.some((line) => line.includes('等 4 条')), false)
  })

  it('is active for a non-default empty rule and tunnel with no hosts', () => {
    const got = rulesMark({
      ...file,
      capture: true,
      capture_empty: 'none',
      capture_https: 'tunnel',
      capture_hosts: [],
    })
    assert.equal(got.active, true)
    assert.deepEqual(got.lines, ['空规则：不抓', '未抓 HTTPS：隧道', '主机：无'])
  })

  it('stays inactive for defaults even when capture is paused', () => {
    const got = rulesMark({ ...file, capture: false, capture_hosts: [] })
    assert.deepEqual(got, {
      active: false,
      lines: ['空规则：抓全部', '未抓 HTTPS：解密', '主机：无'],
    })
  })
})

describe('captureActionLabel', () => {
  it('names the action the button will take', () => {
    assert.equal(captureActionLabel(true), '暂停抓包')
    assert.equal(captureActionLabel(false), '开始抓包')
    assert.equal(captureActionLabel(null), '开始抓包')
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
