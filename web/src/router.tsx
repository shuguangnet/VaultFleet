import { createBrowserRouter } from "react-router-dom";

export const router = createBrowserRouter([
  {
    path: "*",
    lazy: async () => {
      const { AuthGate } = await import("./pages/auth/auth-gate");
      return { Component: AuthGate };
    }
  }
]);
