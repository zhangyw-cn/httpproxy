import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { limitNote } from './src/settings.ts'

describe('limitNote', () => {
  it('is null when the file count matches runtime', () => {
    assert.equal(limitNote(1000, 1000), null)
  })

  it('names the runtime count when they differ', () => {
    assert.equal(limitNote(1000, 50), '本次实际 50')
  })
})
