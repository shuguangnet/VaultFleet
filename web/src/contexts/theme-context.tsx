import { createContext, useContext } from "react";

export type ColorMode = "light" | "dark";

interface ThemeContextValue {
  mode: ColorMode;
  toggleMode: () => void;
}

export const ThemeContext = createContext<ThemeContextValue | null>(null);

export function useColorMode() {
  const context = useContext(ThemeContext);
  if (!context) throw new Error("useColorMode must be used within ThemeContext.Provider");
  return context;
}
