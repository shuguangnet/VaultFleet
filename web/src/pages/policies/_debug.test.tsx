import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp, Select } from "antd";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, vi, beforeAll } from "vitest";

beforeAll(() => {
  if (!globalThis.ResizeObserver) {
    globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
  }
});

describe("debug select 4", () => {
  it("print option html", async () => {
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
    console.log("DROPDOWN HTML:", dropdown?.outerHTML);
  });
});
