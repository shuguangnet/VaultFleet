import { Modal } from "antd";
import type { ReactNode } from "react";

interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  title: ReactNode;
  description: ReactNode;
  confirmText?: string;
  cancelText?: string;
  variant?: "default" | "destructive";
  loading?: boolean;
}

export function ConfirmDialog({
  open,
  onOpenChange,
  onConfirm,
  title,
  description,
  confirmText = "确认",
  cancelText = "取消",
  variant = "destructive",
  loading = false,
}: ConfirmDialogProps) {
  return (
    <Modal
      open={open}
      title={title}
      width="min(92vw, 520px)"
      onCancel={() => onOpenChange(false)}
      onOk={onConfirm}
      okText={confirmText}
      cancelText={cancelText}
      okButtonProps={{
        danger: variant === "destructive",
        loading,
      }}
      cancelButtonProps={{ disabled: loading }}
      centered
      destroyOnHidden
    >
      <div className="vf-confirm-description">{description}</div>
    </Modal>
  );
}
