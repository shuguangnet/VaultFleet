import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "react-router-dom";
import { useEffect, useMemo, useState } from "react";
import { App as AntdApp, ConfigProvider, theme } from "antd";
import zhCN from "antd/locale/zh_CN";
import dayjs from "dayjs";
import "dayjs/locale/zh-cn";
import { router } from "./router";
import { antdTheme } from "./styles/antd-theme";
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
        colorBgLayout: mode === "dark" ? "#111820" : "#f4f7f9",
        colorBgContainer: mode === "dark" ? "#18212b" : "#ffffff",
        colorBorder: mode === "dark" ? "#31404f" : "#dce3e8",
        colorBorderSecondary: mode === "dark" ? "#253240" : "#edf1f4",
        colorTextBase: mode === "dark" ? "#edf3f7" : "#17212b",
        colorText: mode === "dark" ? "#edf3f7" : "#17212b",
        colorTextSecondary: mode === "dark" ? "#a8b5c1" : "#607080",
        colorTextTertiary: mode === "dark" ? "#7f8d9a" : "#8896a4",
        colorTextQuaternary: mode === "dark" ? "#647280" : "#a4afb9",
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
