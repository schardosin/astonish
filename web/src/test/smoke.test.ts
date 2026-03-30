import { describe, it, expect } from 'vitest'

declare const __UI_VERSION__: string

describe('test infrastructure', () => {
  it('vitest runs correctly', () => {
    expect(1 + 1).toBe(2)
  })

  it('globals are defined', () => {
    expect(__UI_VERSION__).toBe('0.0.0-test')
  })
})
