import { describe, expect, it } from 'vitest'
import { decorationVariables, hasNicknameContrast, isSafeDecorationAssetUrl } from '../src/site/utils/decoration-style'
import type { DecorationStyle } from '@gooseforum/client'
const style: DecorationStyle = { code: 'tea', name: 'Tea', kind: 'nicknameStyle', lightColors: ['#166534'], darkColors: ['#86efac'], isEnabled: true, sortOrder: 1 }
describe('decoration rendering boundary', () => {
  it('rejects disabled and arbitrary CSS colors while allowing one-color gradients', () => {
    expect(decorationVariables(style)['--decoration-light']).toBe('linear-gradient(120deg, #166534, #166534)')
    expect(decorationVariables({ ...style, isEnabled: false })).toEqual({})
    expect(decorationVariables({ ...style, lightColors: ['url(https://invalid)'] })).toEqual({})
    expect(decorationVariables({ ...style, darkColors: [] })).toEqual({})
  })
  it('keeps names readable against custom backgrounds and across the gradient', () => {
    expect(hasNicknameContrast(style.lightColors, [255, 255, 255])).toBe(true)
    expect(hasNicknameContrast(style.darkColors, [17, 24, 39])).toBe(true)
    expect(hasNicknameContrast(style.lightColors, [22, 101, 52])).toBe(false)
    expect(hasNicknameContrast(['#000000', '#ffffff'], [255, 255, 255])).toBe(false)
    expect(hasNicknameContrast(['red'], [255, 255, 255])).toBe(false)
  })
})

it('accepts all artwork formats without widening trusted decoration paths', () => {
  for (const ext of ['png', 'webp', 'gif', 'svg']) expect(isSafeDecorationAssetUrl(`/static/decorations/frame.${ext}`)).toBe(true)
  for (const url of ['https://example.com/frame.gif', '/static/decorations/../frame.svg', '/static/decorations/frame.gif?x=1', '/static/decorations/frame.html']) expect(isSafeDecorationAssetUrl(url)).toBe(false)
})
