const WEEKDAYS = ["日", "一", "二", "三", "四", "五", "六"];

export type VisualSchedule =
  | { mode: "daily"; time: string }
  | { mode: "weekly"; time: string; weekdays: number[] }
  | { mode: "monthly"; time: string; monthDay: number }
  | { mode: "interval"; interval: number; unit: "minute" | "hour" }
  | { mode: "custom"; expression: string };

export function scheduleToCron(schedule: VisualSchedule): string {
  if (schedule.mode === "custom") return schedule.expression;
  if (schedule.mode === "interval") {
    const value = Math.max(1, Math.floor(schedule.interval));
    return schedule.unit === "minute" ? `*/${value} * * * *` : `0 */${value} * * *`;
  }
  const [hour, minute] = parseTime(schedule.time);
  if (schedule.mode === "daily") return `${minute} ${hour} * * *`;
  if (schedule.mode === "monthly") {
    const day = Math.min(31, Math.max(1, Math.floor(schedule.monthDay)));
    return `${minute} ${hour} ${day} * *`;
  }
  const weekdays = [...new Set(schedule.weekdays)]
    .filter((day) => Number.isInteger(day) && day >= 0 && day <= 6)
    .sort((a, b) => a - b);
  if (weekdays.length === 0) throw new Error("每周调度至少选择一天");
  return `${minute} ${hour} * * ${weekdays.join(",")}`;
}

export function cronToSchedule(expression: string): VisualSchedule {
  const raw = expression.trim();
  const parts = raw.split(/\s+/);
  if (parts.length !== 5) return { mode: "custom", expression };
  const [minute, hour, day, month, weekday] = parts;

  if (day === "*" && month === "*" && weekday === "*" && /^\*\/\d+$/.test(minute) && hour === "*") {
    return { mode: "interval", interval: Number(minute.slice(2)), unit: "minute" };
  }
  if (day === "*" && month === "*" && weekday === "*" && minute === "0" && /^\*\/\d+$/.test(hour)) {
    return { mode: "interval", interval: Number(hour.slice(2)), unit: "hour" };
  }
  if (!isClockField(hour, 23) || !isClockField(minute, 59) || month !== "*") {
    return { mode: "custom", expression };
  }
  const time = `${hour.padStart(2, "0")}:${minute.padStart(2, "0")}`;
  if (day === "*" && weekday === "*") return { mode: "daily", time };
  if (/^\d+$/.test(day) && weekday === "*" && Number(day) >= 1 && Number(day) <= 31) {
    return { mode: "monthly", time, monthDay: Number(day) };
  }
  if (day === "*" && /^(?:[0-6])(?:,[0-6])*$/.test(weekday)) {
    const weekdays = weekday.split(",").map(Number);
    const canonical = [...new Set(weekdays)].sort((a, b) => a - b);
    if (canonical.join(",") === weekday) return { mode: "weekly", time, weekdays };
  }
  return { mode: "custom", expression };
}

export function isValidCron(expression: string): boolean {
  const raw = expression.trim();
  if (/^@(yearly|annually|monthly|weekly|daily|midnight|hourly|every\s+\S+)$/.test(raw)) return true;
  const parts = raw.split(/\s+/);
  return (parts.length === 5 || parts.length === 6) && parts.every((part) => /^[\d*/?,\-]+$/.test(part));
}

export function describeSchedule(expression: string): string {
  return `${describeCron(expression)}（节点本地时间）`;
}

export function describeRetention(retention: {
  keep_last?: number;
  keep_daily?: number;
  keep_weekly?: number;
  keep_monthly?: number;
}): string {
  const items = [
    ["keep_last", "最近"],
    ["keep_daily", "每日"],
    ["keep_weekly", "每周"],
    ["keep_monthly", "每月"],
  ] as const;
  const enabled = items
    .map(([key, label]) => ({ label, value: Math.max(0, Number(retention[key] ?? 0)) }))
    .filter((item) => item.value > 0)
    .map((item) => `${item.label} ${item.value} 份`);
  return enabled.length ? enabled.join(" · ") : "未配置保留规则";
}

export function describeCron(expr: string): string {
  const schedule = cronToSchedule(expr);
  if (schedule.mode === "daily") return `每天 ${schedule.time}`;
  if (schedule.mode === "weekly") return `每周${schedule.weekdays.map((day) => WEEKDAYS[day]).join("、周")} ${schedule.time}`;
  if (schedule.mode === "monthly") return `每月 ${schedule.monthDay} 日 ${schedule.time}`;
  if (schedule.mode === "interval") return `每 ${schedule.interval} ${schedule.unit === "minute" ? "分钟" : "小时"}`;
  return expr.trim() || "未配置";
}

function parseTime(value: string): [number, number] {
  const match = /^(\d{1,2}):(\d{2})$/.exec(value);
  if (!match) throw new Error("执行时间格式无效");
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  if (hour > 23 || minute > 59) throw new Error("执行时间超出范围");
  return [hour, minute];
}

function isClockField(value: string, max: number): boolean {
  return /^\d+$/.test(value) && Number(value) >= 0 && Number(value) <= max && String(Number(value)) === value;
}
