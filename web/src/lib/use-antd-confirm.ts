import { App } from "antd";
import type { ReactNode } from "react";
import { useCallback } from "react";

interface ConfirmOptions {
  title: ReactNode;
  content: ReactNode;
  okText?: string;
  cancelText?: string;
  okType?: "default" | "primary" | "danger";
  loading?: boolean;
}

export type ConfirmFn = (options: ConfirmOptions) => Promise<boolean>;

export function useAntdConfirm() {
  const { modal } = App.useApp();

  return useCallback(
    (options: ConfirmOptions) =>
      new Promise<boolean>((resolve) => {
        const instance = modal.confirm({
          title: options.title,
          content: options.content,
          okText: options.okText ?? "确认",
          cancelText: options.cancelText ?? "取消",
          okType: options.okType === "danger" ? "danger" : "primary",
          okButtonProps: { loading: options.loading },
          onCancel: () => resolve(false),
          onOk: () => resolve(true),
        });
        // 处理外部 loading 变化的简单更新函数
        return instance;
      }),
    [modal]
  );
}