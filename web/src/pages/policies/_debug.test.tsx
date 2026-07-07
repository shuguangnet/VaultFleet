import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp, Select } from "antd";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

beforeAll(() => {
  if (!globalThis.ResizeObserver) {
    globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
  }
});

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
});

describe("debug select 4", () => {
  it("opens select dropdown", async () => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <AntdApp>
          <Select
            style={{ width: 200 }}
            options={[{ value: "agent-1", label: "node-1" }]}
          />
        </AntdApp>
      </QueryClientProvider>
    );
    await user.click(screen.getByRole("combobox"));
    const dropdown = document.querySelector(".ant-select-dropdown");
    expect(dropdown).toBeTruthy();
  });
});
