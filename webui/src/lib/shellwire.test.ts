import { describe, it, expect } from 'vitest'
import { FrameType, encodeData, encodeResize, parse } from './shellwire'

describe('shellwire', () => {
  it('encodes a data frame as [0x00, ...bytes]', () => {
    const payload = new Uint8Array([0x00, 0xff, 0x80, 'h'.charCodeAt(0)])
    const frame = encodeData(payload)
    expect(frame[0]).toBe(FrameType.Data)
    expect(Array.from(frame.subarray(1))).toEqual(Array.from(payload))
  })

  it('round-trips a data frame with arbitrary non-UTF-8 bytes', () => {
    const payload = new Uint8Array([0x00, 0x01, 0xff, 0xfe, 0x80])
    const parsed = parse(encodeData(payload))
    expect(parsed.type).toBe(FrameType.Data)
    expect(Array.from(parsed.data!)).toEqual(Array.from(payload))
  })

  it('encodes resize as big-endian rows then cols', () => {
    const frame = encodeResize(24, 80)
    expect(Array.from(frame)).toEqual([FrameType.Resize, 0x00, 0x18, 0x00, 0x50])
  })

  it('round-trips resize dimensions', () => {
    const parsed = parse(encodeResize(50, 132))
    expect(parsed.type).toBe(FrameType.Resize)
    expect(parsed.rows).toBe(50)
    expect(parsed.cols).toBe(132)
  })

  it('decodes an exit frame with a signed code', () => {
    // Go's shellwire.Exit(-1) encodes 0xffffffff; parse must read it as int32.
    const msg = new Uint8Array([FrameType.Exit, 0xff, 0xff, 0xff, 0xff])
    const parsed = parse(msg)
    expect(parsed.type).toBe(FrameType.Exit)
    expect(parsed.code).toBe(-1)
  })

  it('decodes a non-negative exit code', () => {
    const msg = new Uint8Array([FrameType.Exit, 0x00, 0x00, 0x00, 0x7f])
    expect(parse(msg).code).toBe(127)
  })

  it('decodes a ping frame with no payload', () => {
    const parsed = parse(new Uint8Array([FrameType.Ping]))
    expect(parsed.type).toBe(FrameType.Ping)
  })

  it('throws on an empty frame', () => {
    expect(() => parse(new Uint8Array())).toThrow(/empty frame/)
  })

  it('throws on a malformed resize payload', () => {
    expect(() => parse(new Uint8Array([FrameType.Resize, 0x00]))).toThrow(/resize payload/)
  })
})
