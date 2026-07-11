/**
 * VaultFleet 设计 token 集中定义。
 * 所有页面与组件应优先引用此处常量，避免直接硬编码 hex 颜色。
 */

export const colors = {
  /** 主品牌色：按钮、链接、重点标识 */
  primary: "#0f766e",
  /** 主色悬停态 */
  primaryHover: "#115e59",
  /** 信息蓝：信息提示、图表 */
  info: "#2563eb",
  /** 成功绿：在线/成功状态 */
  success: "#10b981",
  /** 警示橙：警告/待同步 */
  warning: "#f59e0b",
  /** 危险红：失败/错误 */
  error: "#ef4444",
  /** 页面底色 */
  background: "#f3f6f7",
  /** 卡片/面板背景 */
  card: "#ffffff",
  /** 浮层/侧栏背景 */
  elevated: "#ffffff",
  /** 主边框 */
  border: "#d9e1e3",
  /** 次级边框/表头背景 */
  borderSecondary: "#eaf0f1",
  /** 主文字 */
  text: "#182329",
  /** 次级文字 */
  textSecondary: "#607077",
  /** 三级文字/placeholder */
  textTertiary: "#88979d",
  /** 侧栏背景 */
  siderBg: "#172126",
  /** 侧栏选中 indicator */
  siderIndicator: "#5eead4",
  /** 输入框悬停边框 */
  inputHoverBorder: "#94a3b8",
  /** 输入框聚焦 outline */
  inputFocusOutline: "rgba(15, 118, 110, 0.14)",
} as const;

export const chartColors = {
  primary: colors.primary,
  info: colors.info,
  success: colors.success,
  warning: colors.warning,
  error: colors.error,
  cyan: "#0891b2",
  purple: "#8b5cf6",
  slate: colors.textSecondary,
} as const;

export const shadows = {
  card: "0 1px 3px rgba(15, 23, 42, 0.06), 0 1px 2px rgba(15, 23, 42, 0.04)",
  cardHover: "0 4px 12px rgba(15, 23, 42, 0.08)",
  header: "0 1px 3px rgba(15, 23, 42, 0.06)",
  modal: "0 20px 60px rgba(15, 23, 42, 0.16)",
  login: "0 24px 80px rgba(15, 23, 42, 0.18)",
} as const;

export const borderRadius = {
  xs: 4,
  sm: 6,
  DEFAULT: 8,
  lg: 12,
  xl: 16,
} as const;

export const spacing = {
  pagePaddingX: 28,
  pagePaddingY: 24,
  pagePaddingMobile: 14,
  cardPadding: 20,
  cardPaddingLarge: 24,
  gutter: 20,
} as const;
