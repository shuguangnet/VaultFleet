import type { ThemeConfig } from "antd";

export const antdTheme: ThemeConfig = {
  token: {
    colorPrimary: "#1668dc",
    colorInfo: "#1668dc",
    colorSuccess: "#52c41a",
    colorWarning: "#faad14",
    colorError: "#ff4d4f",
    colorLink: "#1668dc",
    borderRadius: 6,
    fontSize: 14,
    wireframe: false,
    controlHeight: 32,
    controlHeightSM: 28,
    controlHeightLG: 40,
    boxShadow:
      "0 1px 2px 0 rgba(0, 0, 0, 0.03), 0 1px 6px -1px rgba(0, 0, 0, 0.02), 0 2px 4px 0 rgba(0, 0, 0, 0.02)",
    boxShadowSecondary:
      "0 6px 16px 0 rgba(0, 0, 0, 0.08), 0 3px 6px -4px rgba(0, 0, 0, 0.12), 0 9px 28px 8px rgba(0, 0, 0, 0.05)",
  },
  components: {
    Layout: {
      headerBg: "#ffffff",
      siderBg: "#0f1f3d",
      bodyBg: "#f5f7fa",
      headerHeight: 56,
      headerPadding: "0 20px",
    },
    Menu: {
      darkItemBg: "transparent",
      darkSubMenuItemBg: "transparent",
      darkItemSelectedBg: "rgba(22, 104, 220, 0.85)",
      darkItemHoverBg: "rgba(255, 255, 255, 0.08)",
      itemColor: "rgba(255, 255, 255, 0.65)",
      itemSelectedColor: "#ffffff",
      itemHoverColor: "#ffffff",
    },
    Card: {
      borderRadiusLG: 6,
    },
    Table: {
      headerBg: "#fafafa",
      headerColor: "rgba(0, 0, 0, 0.88)",
      headerSortActiveBg: "#f0f0f0",
      rowHoverBg: "#fafafa",
      borderColor: "#f0f0f0",
    },
    Button: {
      primaryShadow: "none",
      defaultShadow: "none",
      fontWeight: 400,
    },
    Modal: {
      borderRadiusLG: 8,
    },
    Drawer: {
      paddingLG: 24,
    },
    Form: {
      itemMarginBottom: 16,
    },
    Tabs: {
      cardBg: "transparent",
    },
  },
};