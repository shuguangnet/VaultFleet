import type { ThemeConfig } from "antd";

export const antdTheme: ThemeConfig = {
  token: {
    colorPrimary: "#1f4f8f",
    colorInfo: "#1f4f8f",
    colorSuccess: "#2f855a",
    colorWarning: "#b7791f",
    colorError: "#c53030",
    colorLink: "#1f4f8f",
    colorBgLayout: "#f6f7f9",
    colorBgContainer: "#ffffff",
    colorBorder: "#d8dee8",
    colorBorderSecondary: "#e7ebf0",
    colorTextBase: "#111827",
    borderRadius: 4,
    fontSize: 14,
    wireframe: false,
    controlHeight: 32,
    controlHeightSM: 28,
    controlHeightLG: 40,
    boxShadow: "0 1px 2px rgba(15, 23, 42, 0.04)",
    boxShadowSecondary: "0 8px 24px rgba(15, 23, 42, 0.10)",
  },
  components: {
    Layout: {
      headerBg: "#ffffff",
      siderBg: "#172033",
      bodyBg: "#f6f7f9",
      headerHeight: 56,
      headerPadding: "0 20px",
    },
    Menu: {
      darkItemBg: "transparent",
      darkSubMenuItemBg: "transparent",
      darkItemSelectedBg: "rgba(255, 255, 255, 0.10)",
      darkItemHoverBg: "rgba(255, 255, 255, 0.06)",
      itemColor: "rgba(255, 255, 255, 0.68)",
      itemSelectedColor: "#ffffff",
      itemHoverColor: "#ffffff",
    },
    Card: {
      borderRadiusLG: 4,
      paddingLG: 20,
    },
    Table: {
      headerBg: "#f8fafc",
      headerColor: "#111827",
      headerSortActiveBg: "#eef2f7",
      rowHoverBg: "#f8fafc",
      borderColor: "#e7ebf0",
    },
    Button: {
      primaryShadow: "none",
      defaultShadow: "none",
      fontWeight: 400,
    },
    Modal: {
      borderRadiusLG: 4,
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
