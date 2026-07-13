import { useCallback, useEffect, useRef, useState } from "react";
import {
  Alert,
  Button,
  Checkbox,
  Empty,
  Skeleton,
  Space,
  Tooltip,
  Typography,
} from "antd";
import {
  FileOutlined,
  FolderOpenOutlined,
  FolderOutlined,
  HomeOutlined,
  ReloadOutlined,
  RightOutlined,
  CaretDownOutlined,
} from "@ant-design/icons";
import { browseAgent, dirSizeAgent } from "@/services/agents";
import type { BrowseEntry } from "@/types/api";

interface DirectoryBrowserProps {
  agentId: string;
  onSelect: (path: string) => void;
  onDeselect?: (path: string) => void;
  selectedPaths?: string[];
  className?: string;
}

interface TreeNode {
  entry: BrowseEntry;
  children: TreeNode[] | null;
  loading: boolean;
  expanded: boolean;
  error?: string;
}

function sortEntries(entries: BrowseEntry[]): BrowseEntry[] {
  return [...entries].sort((a, b) => {
    if (a.type !== b.type) return a.type === "dir" ? -1 : 1;
    return a.path.localeCompare(b.path);
  });
}

function formatSize(bytes: number): string {
  if (bytes < 0) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

function entriesToNodes(entries: BrowseEntry[]): TreeNode[] {
  return sortEntries(entries).map((e) => ({
    entry: e,
    children: null,
    loading: false,
    expanded: false,
  }));
}

export function DirectoryBrowser({
  agentId,
  onSelect,
  onDeselect,
  selectedPaths = [],
}: DirectoryBrowserProps) {
  const [nodes, setNodes] = useState<TreeNode[]>([]);
  const [rootLoading, setRootLoading] = useState(true);
  const [rootError, setRootError] = useState<string | null>(null);
  const inflightRef = useRef<Set<string>>(new Set());
  const [dirSizes, setDirSizes] = useState<
    Map<string, number | "loading" | "error">
  >(new Map());

  const fetchChildren = useCallback(
    async (path: string): Promise<BrowseEntry[]> => {
      const resp = await browseAgent(agentId, { path, depth: 1 });
      return resp.entries || [];
    },
    [agentId]
  );

  const loadRoot = useCallback(async () => {
    setRootLoading(true);
    setRootError(null);
    setDirSizes(new Map());
    try {
      const entries = await fetchChildren("/");
      setNodes(entriesToNodes(entries));
    } catch (err: any) {
      setRootError(err?.message || "加载失败");
    } finally {
      setRootLoading(false);
    }
  }, [fetchChildren]);

  useEffect(() => {
    loadRoot();
  }, [loadRoot]);

  const updateNodeAtPath = useCallback(
    (
      nodeList: TreeNode[],
      targetPath: string,
      updater: (n: TreeNode) => TreeNode
    ): TreeNode[] => {
      return nodeList.map((node) => {
        if (node.entry.path === targetPath) return updater(node);
        if (node.children && targetPath.startsWith(node.entry.path + "/")) {
          return {
            ...node,
            children: updateNodeAtPath(node.children, targetPath, updater),
          };
        }
        return node;
      });
    },
    []
  );

  const handleToggle = useCallback(
    (path: string) => {
      setNodes((prev) => {
        const node = findNode(prev, path);
        if (!node || node.entry.type !== "dir") return prev;
        if (node.expanded) {
          return updateNodeAtPath(prev, path, (n) => ({ ...n, expanded: false }));
        }
        if (node.children !== null) {
          return updateNodeAtPath(prev, path, (n) => ({ ...n, expanded: true }));
        }
        if (inflightRef.current.has(path)) {
          return updateNodeAtPath(prev, path, (n) => ({ ...n, expanded: true }));
        }
        inflightRef.current.add(path);
        fetchChildren(path)
          .then((entries) => {
            setNodes((p) =>
              updateNodeAtPath(p, path, (n) => ({
                ...n,
                children: entriesToNodes(entries),
                loading: false,
              }))
            );
          })
          .catch((err: any) => {
            setNodes((p) =>
              updateNodeAtPath(p, path, (n) => ({
                ...n,
                loading: false,
                error: err?.message || "加载失败",
              }))
            );
          })
          .finally(() => inflightRef.current.delete(path));
        return updateNodeAtPath(prev, path, (n) => ({
          ...n,
          loading: true,
          expanded: true,
        }));
      });
    },
    [fetchChildren, updateNodeAtPath]
  );

  const isSelected = useCallback(
    (path: string) => selectedPaths.includes(path),
    [selectedPaths]
  );

  const handleCalcSize = useCallback(
    async (path: string) => {
      setDirSizes((prev) => {
        const next = new Map(prev);
        next.set(path, "loading");
        return next;
      });
      try {
        const resp = await dirSizeAgent(agentId, { path });
        setDirSizes((prev) => {
          const next = new Map(prev);
          next.set(path, resp.error && !resp.size ? "error" : resp.size ?? 0);
          return next;
        });
      } catch {
        setDirSizes((prev) => {
          const next = new Map(prev);
          next.set(path, "error");
          return next;
        });
      }
    },
    [agentId]
  );

  return (
    <div className="vf-browser">
      <div className="vf-browser-toolbar">
        <Space>
          <Button
            type="text"
            size="small"
            icon={<HomeOutlined />}
            onClick={loadRoot}
            disabled={rootLoading}
          />
          <div className="vf-browser-path">
            /
          </div>
          <Button
            type="text"
            size="small"
            icon={<ReloadOutlined spin={rootLoading} />}
            onClick={loadRoot}
            disabled={rootLoading}
          />
        </Space>
      </div>

      <div style={{ height: 300, overflowY: "auto" }}>
        {rootError ? (
          <div style={{ padding: 16 }}>
            <Alert
              type="error"
              showIcon
              message="无法浏览目录"
              description={rootError}
            />
          </div>
        ) : rootLoading ? (
          <div style={{ padding: 8 }}>
            {[1, 2, 3, 4, 5].map((i) => (
              <Skeleton.Input key={i} active size="small" block style={{ height: 32, marginBottom: 4 }} />
            ))}
          </div>
        ) : nodes.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="目录为空"
            style={{ marginTop: 40 }}
          />
        ) : (
          <div style={{ padding: "4px 0" }}>
            {nodes.map((node) => (
              <TreeNodeRow
                key={node.entry.path}
                node={node}
                depth={0}
                isSelected={isSelected}
                onToggle={handleToggle}
                onCheck={(p, checked) =>
                  checked ? onSelect(p) : onDeselect?.(p)
                }
                onCalcSize={handleCalcSize}
                dirSizes={dirSizes}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function TreeNodeRow({
  node,
  depth,
  isSelected,
  onToggle,
  onCheck,
  onCalcSize,
  dirSizes,
}: {
  node: TreeNode;
  depth: number;
  isSelected: (path: string) => boolean;
  onToggle: (path: string) => void;
  onCheck: (path: string, checked: boolean) => void;
  onCalcSize: (path: string) => void;
  dirSizes: Map<string, number | "loading" | "error">;
}) {
  const checked = isSelected(node.entry.path);
  const name =
    node.entry.path.split("/").filter(Boolean).pop() || node.entry.path;
  const isDir = node.entry.type === "dir";
  const dirSize = isDir ? dirSizes.get(node.entry.path) : undefined;

  return (
    <>
      <div
        className="vf-tree-row"
        style={{
          display: "flex",
          alignItems: "center",
          gap: 4,
          padding: "4px 8px",
          paddingLeft: depth * 20 + 8,
          cursor: isDir ? "pointer" : "default",
        }}
      >
        {isDir ? (
          <button
            type="button"
            onClick={() => onToggle(node.entry.path)}
            style={{
              background: "transparent",
              border: "none",
              padding: 0,
              cursor: "pointer",
              fontSize: 12,
              color: "var(--vf-text-muted)",
              width: 16,
            }}
          >
            {node.loading ? (
              <ReloadOutlined spin style={{ fontSize: 10 }} />
            ) : node.expanded ? (
              <CaretDownOutlined style={{ fontSize: 10 }} />
            ) : (
              <RightOutlined style={{ fontSize: 10 }} />
            )}
          </button>
        ) : (
          <span style={{ width: 16 }} />
        )}

        <span
          onClick={() => isDir && onToggle(node.entry.path)}
          style={{
            display: "flex",
            alignItems: "center",
            gap: 6,
            flex: 1,
            minWidth: 0,
          }}
        >
          {isDir ? (
            node.expanded ? (
              <FolderOpenOutlined style={{ color: "var(--vf-primary)", fontSize: 14 }} />
            ) : (
              <FolderOutlined style={{ color: "var(--vf-primary)", fontSize: 14 }} />
            )
          ) : (
            <FileOutlined style={{ color: "var(--vf-text-muted)", fontSize: 14 }} />
          )}
          <Typography.Text ellipsis style={{ fontSize: 13, flex: 1 }}>
            {name}
          </Typography.Text>
        </span>

        {!isDir && node.entry.size > 0 && (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {formatSize(node.entry.size)}
          </Typography.Text>
        )}

        {isDir && dirSize === undefined && (
          <Tooltip title="计算目录大小">
            <Button
              type="text"
              size="small"
              onClick={(e) => {
                e.stopPropagation();
                onCalcSize(node.entry.path);
              }}
            />
          </Tooltip>
        )}
        {isDir && dirSize === "loading" && (
          <ReloadOutlined spin style={{ fontSize: 10, color: "var(--vf-text-muted)" }} />
        )}
        {isDir && dirSize === "error" && (
          <Typography.Text type="danger" style={{ fontSize: 12 }}>
            错误
          </Typography.Text>
        )}
        {isDir && typeof dirSize === "number" && (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {formatSize(dirSize)}
          </Typography.Text>
        )}

        <Checkbox
          checked={checked}
          onChange={(e) => onCheck(node.entry.path, e.target.checked)}
          style={{ marginLeft: 4 }}
        />
      </div>

      {node.expanded && node.error && (
        <div
          style={{
            padding: "2px 8px",
            paddingLeft: (depth + 1) * 20 + 8,
            fontSize: 12,
            color: "#ef4444",
          }}
        >
          {node.error}
        </div>
      )}

      {node.expanded && node.children && node.children.length === 0 && !node.loading && (
        <div
          style={{
            padding: "2px 8px",
            paddingLeft: (depth + 1) * 20 + 8,
            fontSize: 12,
            color: "var(--vf-text-muted)",
          }}
        >
          空目录
        </div>
      )}

      {node.expanded &&
        node.children?.map((child) => (
          <TreeNodeRow
            key={child.entry.path}
            node={child}
            depth={depth + 1}
            isSelected={isSelected}
            onToggle={onToggle}
            onCheck={onCheck}
            onCalcSize={onCalcSize}
            dirSizes={dirSizes}
          />
        ))}
    </>
  );
}

function findNode(nodes: TreeNode[], path: string): TreeNode | null {
  for (const node of nodes) {
    if (node.entry.path === path) return node;
    if (node.children) {
      const found = findNode(node.children, path);
      if (found) return found;
    }
  }
  return null;
}
