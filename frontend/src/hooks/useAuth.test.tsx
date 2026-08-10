// @vitest-environment jsdom

import type { ReactNode } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { useCurrentUser } from "./useAuth"

vi.mock("@/lib/api", () => ({
  api: {
    me: vi.fn(),
  },
}))

const mockedMe = vi.mocked(api.me)

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

describe("useCurrentUser", () => {
  beforeEach(() => {
    mockedMe.mockReset()
    mockedMe.mockResolvedValue({
      data: {
        sub: "test-user",
        name: "Test User",
        email: "test@example.com",
        role: "viewer",
      },
    })
  })

  it.each([undefined, false])("does not probe /auth/me when auth is %s", async (authEnabled) => {
    renderHook(() => useCurrentUser(authEnabled), { wrapper })

    await Promise.resolve()
    expect(mockedMe).not.toHaveBeenCalled()
  })

  it("loads the current user when auth is enabled", async () => {
    renderHook(() => useCurrentUser(true), { wrapper })

    await waitFor(() => expect(mockedMe).toHaveBeenCalledOnce())
  })
})
