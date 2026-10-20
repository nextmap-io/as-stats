// @vitest-environment jsdom

import { cleanup, render } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it } from "vitest"
import { ASRef } from "./ASRef"
import { PRIVATE_AS_GROUP, PRIVATE_AS_GROUP_NAME } from "@/lib/asn"

afterEach(cleanup)

describe("ASRef", () => {
  it("renders the private-use group as a labelled, non-clickable row", () => {
    const { container } = render(
      <MemoryRouter>
        <ASRef asn={PRIVATE_AS_GROUP} search="?period=1h" />
      </MemoryRouter>,
    )
    expect(container.textContent).toBe(PRIVATE_AS_GROUP_NAME)
    expect(container.querySelector("a")).toBeNull()
    expect(container.textContent).not.toContain("4294967295")
  })
  it("accepts the sentinel as a string key (conversations, movers)", () => {
    const { container } = render(
      <MemoryRouter>
        <ASRef asn={String(PRIVATE_AS_GROUP)} />
      </MemoryRouter>,
    )
    expect(container.querySelector("a")).toBeNull()
  })
  it("links any other ASN, private ones included", () => {
    const { container } = render(
      <MemoryRouter>
        <ASRef asn={65001} search="?period=1h" />
      </MemoryRouter>,
    )
    const a = container.querySelector("a")
    expect(a?.getAttribute("href")).toBe("/as/65001?period=1h")
    expect(a?.textContent).toBe("AS65001")
  })
})
