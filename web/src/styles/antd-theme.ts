import type { ThemeConfig } from "antd";
import { colors } from "./theme-tokens";

export const antdTheme: ThemeConfig = {
  token: {
    colorPrimary: colors.primary,
    colorInfo: colors.info,
    colorSuccess: colors.success,
    colorWarning: colors.warning,
    colorError: colors.error,
    colorLink: colors.primary,
    colorBgLayout: colors.background,
    colorBgContainer: colors.card,
    colorBorder: colors.border,
    colorBorderSecondary: colors.borderSecondary,
    colorTextBase: colors.text,
    borderRadius: 8,
    borderRadiusLG: 12,
    borderRadiusSM: 6,
    borderRadiusXS: 4,
    fontSize: 14,
    wireframe: false,
    controlHeight: 34,
    controlHeightSM: 30,
    controlHeightLG: 42,
    boxShadow:
      "0 1px 3px rgba(15, 23, 42, 0.06), 0 1px 2px rgba(15, 23, 42, 0.04)",
    boxShadowSecondary: "0 8px 28px rgba(15, 23, 42, 0.10)",
    boxShadowTertiary: "0 20px 60px rgba(15, 23, 42, 0.16)",
    motion: true,
  },
  components: {
    Layout: {
      headerBg: colors.card,
      siderBg: colors.siderBg,
      bodyBg: colors.background,
      headerHeight: 56,
      headerPadding: "0 20px",
    },
    Menu: {
      darkItemBg: "transparent",
      darkSubMenuItemBg: "transparent",
      darkItemSelectedBg: "rgba(255, 255, 255, 0.10)",
      darkItemHoverBg: "rgba(255, 255, 255, 0.06)",
      itemColor: "rgba(255, 255, 255, 0.65)",
      itemSelectedColor: "#ffffff",
      itemHoverColor: "#ffffff",
      itemBorderRadius: 8,
    },
    Card: {
      borderRadiusLG: 12,
      paddingLG: 20,
      boxShadow:
        "0 1px 3px rgba(15, 23, 42, 0.06), 0 1px 2px rgba(15, 23, 42, 0.04)",
      boxShadowTertiary: "0 20px 60px rgba(15, 23, 42, 0.16)",
    },
    Table: {
      headerBg: colors.background,
      headerColor: colors.text,
      headerSortActiveBg: colors.borderSecondary,
      rowHoverBg: colors.background,
      borderColor: colors.border,
      borderRadius: 8,
    },
    Button: {
      primaryShadow: "none",
      defaultShadow: "none",
      fontWeight: 500,
      borderRadius: 8,
    },
    Modal: {
      borderRadiusLG: 12,
      boxShadow: "0 20px 60px rgba(15, 23, 42, 0.16)",
    },
    Drawer: {
      paddingLG: 24,
      borderRadius: 0,
    },
    Form: {
      itemMarginBottom: 18,
    },
    Tabs: {
      cardBg: "transparent",
    },
    Input: {
      borderRadius: 8,
    },
    Select: {
      borderRadius: 8,
    },
    Tag: {
      borderRadius: 6,
    },
    Avatar: {
      borderRadius: 8,
    },
    Breadcrumb: {
      colorText: colors.textSecondary,
      colorTextDescription: colors.textTertiary,
      colorBgTextHover: "rgba(15, 76, 129, 0.06)",
    },
    Tooltip: {
      borderRadius: 6,
    },
    Alert: {
      borderRadius: 8,
    },
  },
};
