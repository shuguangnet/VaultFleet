import { describe, expect, it } from "vitest";
import { cronToSchedule, describeRetention, describeSchedule, isValidCron, scheduleToCron } from "./cron";

describe("visual schedules", () => {
  it("generates daily, weekly, monthly, and interval cron", () => {
    expect(scheduleToCron({ mode: "daily", time: "02:30" })).toBe("30 2 * * *");
    expect(scheduleToCron({ mode: "weekly", time: "03:00", weekdays: [5, 1, 3] })).toBe("0 3 * * 1,3,5");
    expect(scheduleToCron({ mode: "monthly", time: "01:00", monthDay: 15 })).toBe("0 1 15 * *");
    expect(scheduleToCron({ mode: "interval", interval: 6, unit: "hour" })).toBe("0 */6 * * *");
  });

  it("recognizes only lossless visual schedules", () => {
    expect(cronToSchedule("0 3 * * 1,3,5")).toEqual({ mode: "weekly", time: "03:00", weekdays: [1, 3, 5] });
    expect(cronToSchedule("0 3 * * 5,1")).toEqual({ mode: "custom", expression: "0 3 * * 5,1" });
    expect(cronToSchedule("0 3 1-7 * 1").mode).toBe("custom");
    expect(scheduleToCron(cronToSchedule("30 2 * * *"))).toBe("30 2 * * *");
  });

  it("validates legacy formats and rejects malformed expressions", () => {
    expect(isValidCron("0 2 * * *")).toBe(true);
    expect(isValidCron("0 0 2 * * *")).toBe(true);
    expect(isValidCron("@every 6h")).toBe(true);
    expect(isValidCron("not a cron")).toBe(false);
  });

  it("describes timezone and retention tiers", () => {
    expect(describeSchedule("0 2 * * *")).toContain("节点本地时间");
    expect(describeRetention({ keep_last: 7, keep_daily: 0, keep_weekly: 4 })).toBe("最近 7 份 · 每周 4 份");
  });
});
