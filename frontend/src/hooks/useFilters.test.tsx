// @vitest-environment jsdom

import type { PropsWithChildren } from "react"
import { act, renderHook } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { FILTER_WINDOW_REFRESH_MS, useFilters } from "./useFilters"

function wrapperFor(entry: string) {
  return function Wrapper({ children }: PropsWithChildren) {
    return <MemoryRouter initialEntries={[entry]}>{children}</MemoryRouter>
  }
}

describe("useFilters relative windows", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date("2026-08-10T12:00:00.000Z"))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it("advances client-resolved from/to filters with the refresh cadence", () => {
    const { result } = renderHook(() => useFilters(), {
      wrapper: wrapperFor("/?period=15m"),
    })

    const initialFrom = result.current.filters.from
    const initialTo = result.current.filters.to
    expect(initialFrom).toBe("2026-08-10T11:45:00.000Z")
    expect(initialTo).toBe("2026-08-10T12:00:00.000Z")

    act(() => {
      vi.advanceTimersByTime(FILTER_WINDOW_REFRESH_MS)
    })

    expect(Date.parse(result.current.filters.from!)).toBe(Date.parse(initialFrom!) + FILTER_WINDOW_REFRESH_MS)
    expect(Date.parse(result.current.filters.to!)).toBe(Date.parse(initialTo!) + FILTER_WINDOW_REFRESH_MS)
  })

  it("advances chart bounds for backend-resolved presets without replacing period", () => {
    const { result } = renderHook(() => useFilters(), {
      wrapper: wrapperFor("/?period=1h"),
    })

    const initialBounds = result.current.timeBounds
    expect(result.current.filters).toMatchObject({ period: "1h" })
    expect(result.current.filters.from).toBeUndefined()

    act(() => {
      vi.advanceTimersByTime(FILTER_WINDOW_REFRESH_MS)
    })

    expect(result.current.filters).toMatchObject({ period: "1h" })
    expect(result.current.timeBounds.from).toBe(initialBounds.from + FILTER_WINDOW_REFRESH_MS)
    expect(result.current.timeBounds.to).toBe(initialBounds.to + FILTER_WINDOW_REFRESH_MS)
  })

  it("keeps explicit from/to links immutable", () => {
    const from = "2026-08-01T10:00:00.000Z"
    const to = "2026-08-01T11:00:00.000Z"
    const { result } = renderHook(() => useFilters(), {
      wrapper: wrapperFor(`/?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`),
    })

    act(() => {
      vi.advanceTimersByTime(FILTER_WINDOW_REFRESH_MS * 3)
    })

    expect(result.current.filters.from).toBe(from)
    expect(result.current.filters.to).toBe(to)
    expect(result.current.timeBounds).toEqual({
      from: Date.parse(from),
      to: Date.parse(to),
    })
  })
})
