import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import type { TaskHistory } from "@/types/task";
import { formatBytes, formatDuration, formatSpeed, renderTaskMetricContent } from "./tasks-page";

afterEach(() => {
  cleanup();
});

describe("task progress helpers", () => {
  it("formats byte counts using compact units", () => {
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(2 * 1024 * 1024)).toBe("2.0 MB");
    expect(formatBytes(3 * 1024 * 1024 * 1024)).toBe("3.00 GB");
    expect(formatBytes(4 * 1024 * 1024 * 1024 * 1024)).toBe("4.00 TB");
  });

  it("formats upload speed only when positive", () => {
    expect(formatSpeed(0)).toBe("");
    expect(formatSpeed(-1)).toBe("");
    expect(formatSpeed(512)).toBe("512 B/s");
    expect(formatSpeed(512 * 1024)).toBe("512.0 KB/s");
    expect(formatSpeed(2 * 1024 * 1024)).toBe("2.0 MB/s");
  });

  it("formats task durations as mm:ss or h:mm:ss", () => {
    expect(formatDuration(0)).toBe("0s");
    expect(formatDuration(59_999)).toBe("59s");
    expect(formatDuration(60_000)).toBe("1:00");
    expect(formatDuration(3_661_000)).toBe("1:01:01");
  });

  it("renders active backup progress with percent and upload speed", () => {
    render(
      <>
        {renderTaskMetricContent(
          task({
            status: "running",
            progress: {
              agent_id: "agent-1",
              phase: "backup",
              percent_done: 0.424,
              total_files: 100,
              files_done: 42,
              total_bytes: 10 * 1024 * 1024,
              bytes_done: 4 * 1024 * 1024,
              bytes_per_sec: 2 * 1024 * 1024,
              current_file: "/data/archive.tar",
            },
          }),
        )}
      </>,
    );

    expect(screen.getByText("上传中: 4.0 MB / 10.0 MB (42%)")).toBeInTheDocument();
    expect(screen.getByText("↑2.0 MB/s")).toBeInTheDocument();
  });

  it("renders percentage-scale progress values without multiplying again", () => {
    render(
      <>
        {renderTaskMetricContent(
          task({
            status: "running",
            progress: {
              agent_id: "agent-1",
              phase: "backup",
              percent_done: 64.5,
              total_files: 0,
              files_done: 0,
              total_bytes: 4096,
              bytes_done: 2048,
              bytes_per_sec: 0,
              current_file: "",
            },
          }),
        )}
      </>,
    );

    expect(screen.getByText("上传中: 2.0 KB / 4.0 KB (65%)")).toBeInTheDocument();
    expect(screen.queryByText(/^↑/)).not.toBeInTheDocument();
  });

  it("renders active task fallback states and completed metrics", () => {
    const { rerender } = render(<>{renderTaskMetricContent(task({ status: "pending" }))}</>);
    expect(screen.getByText("准备中...")).toBeInTheDocument();

    rerender(
      <>
        {renderTaskMetricContent(
          task({
            status: "running",
            progress: {
              agent_id: "agent-1",
              phase: "forget",
              percent_done: 0,
              total_files: 0,
              files_done: 0,
              total_bytes: 0,
              bytes_done: 0,
              bytes_per_sec: 0,
              current_file: "",
            },
          }),
        )}
      </>,
    );
    expect(screen.getByText("清理旧快照...")).toBeInTheDocument();

    rerender(<>{renderTaskMetricContent(task({ status: "success", duration_ms: 1530, repo_size: 5 * 1024 * 1024 }))}</>);
    expect(screen.getByText("1s")).toBeInTheDocument();
    expect(screen.getByText("5.0 MB")).toBeInTheDocument();

    rerender(<>{renderTaskMetricContent(task({ status: "success", duration_ms: 1530, repo_size: 4096 }))}</>);
    expect(screen.getByText("4.0 KB")).toBeInTheDocument();

    rerender(<>{renderTaskMetricContent(task({ status: "success", duration_ms: 1530, repo_size: 3 * 1024 * 1024 * 1024 }))}</>);
    expect(screen.getByText("3.00 GB")).toBeInTheDocument();

    rerender(<>{renderTaskMetricContent(task({ status: "success", duration_ms: 1530, repo_size: 4 * 1024 * 1024 * 1024 * 1024 }))}</>);
    expect(screen.getByText("4.00 TB")).toBeInTheDocument();
  });

  it("renders a cancel button for active tasks when a cancel callback is provided", async () => {
    const user = userEvent.setup();
    let cancelledTaskId: string | null = null;

    render(<>{renderTaskMetricContent(task({ id: "task-42", status: "running" }), (taskId) => { cancelledTaskId = taskId; })}</>);

    await user.click(screen.getByRole("button", { name: "取消任务" }));

    expect(cancelledTaskId).toBe("task-42");
  });

  it("renders cancelled tasks with formatted duration", () => {
    render(<>{renderTaskMetricContent(task({ status: "cancelled", duration_ms: 65_000 }))}</>);

    expect(screen.getByText("1:05")).toBeInTheDocument();
  });

  it("renders completed verification task metrics", () => {
    render(
      <>
        {renderTaskMetricContent(
          task({
            type: "verify",
            status: "success",
            duration_ms: 12_000,
            verification: {
              status: "passed",
              snapshot_id: "snap-1",
              checks: [
                {
                  code: "restic_check",
                  status: "passed",
                  severity: "info",
                  message: "repository check passed",
                  duration_ms: 10,
                },
              ],
            },
          }),
        )}
      </>,
    );

    expect(screen.getByText("12s")).toBeInTheDocument();
  });
});

function task(overrides: Partial<TaskHistory> = {}): TaskHistory {
  return {
    id: "task-1",
    message_id: "msg-1",
    agent_id: "agent-1",
    type: "backup",
    status: "success",
    created_at: "2026-05-25T00:00:00Z",
    ...overrides,
  };
}
