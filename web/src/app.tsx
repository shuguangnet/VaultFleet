import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "react-router-dom";
import { useEffect, useMemo, useState } from "react";
import { App as AntdApp, ConfigProvider, theme } from "antd";
import zhCN from "antd/locale/zh_CN";
import dayjs from "dayjs";
import "dayjs/locale/zh-cn";
import { router } from "./router";
import { antdTheme } from "./styles/antd-theme";
import { darkColors } from "./styles/theme-tokens";
import { ThemeContext, type ColorMode } from "./contexts/theme-context";

dayjs.locale("zh-cn");

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false
    }
  }
});

export function App() {
  const [mode, setMode] = useState<ColorMode>(() => {
    const saved = localStorage.getItem("vaultfleet-color-mode");
    if (saved === "light" || saved === "dark") return saved;
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  });

  useEffect(() => {
    document.documentElement.dataset.theme = mode;
    document.documentElement.style.colorScheme = mode;
    localStorage.setItem("vaultfleet-color-mode", mode);
  }, [mode]);

  const themeConfig = useMemo(
    () => ({
      ...antdTheme,
      algorithm: mode === "dark" ? theme.darkAlgorithm : theme.defaultAlgorithm,
      token: {
        ...antdTheme.token,
        colorPrimary: mode === "dark" ? darkColors.primary : "#0f766e",
        colorLink: mode === "dark" ? darkColors.primaryHover : "#0f766e",
        colorBgLayout: mode === "dark" ? darkColors.background : "#f3f6f7",
        colorBgContainer: mode === "dark" ? darkColors.card : "#ffffff",
        colorBorder: mode === "dark" ? darkColors.border : "#d9e1e3",
        colorBorderSecondary: mode === "dark" ? darkColors.borderSecondary : "#eaf0f1",
        colorTextBase: mode === "dark" ? darkColors.text : "#182329",
        colorText: mode === "dark" ? darkColors.text : "#182329",
        colorTextSecondary: mode === "dark" ? darkColors.textSecondary : "#607077",
        colorTextTertiary: mode === "dark" ? darkColors.textTertiary : "#88979d",
        colorTextQuaternary: mode === "dark" ? darkColors.textQuaternary : "#a4afb9",
      },
    }),
    [mode]
  );

  return (
    <ThemeContext.Provider value={{ mode, toggleMode: () => setMode((value) => value === "light" ? "dark" : "light") }}>
      <ConfigProvider locale={zhCN} theme={themeConfig}>
        <AntdApp>
          <QueryClientProvider client={queryClient}>
            <RouterProvider router={router} />
          </QueryClientProvider>
        </AntdApp>
      </ConfigProvider>
    </ThemeContext.Provider>
  );
}
