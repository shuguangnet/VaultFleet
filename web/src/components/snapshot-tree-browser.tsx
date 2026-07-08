import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Alert,
  Button,
  Checkbox,
  Empty,
  Skeleton,
  Tooltip,
  Typography,
} from "antd";
import {
  CaretDownOutlined,
  FileOutlined,
  FolderOpenOutlined,
  FolderOutlined,
  ReloadOutlined,
  RightOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import { browseSnapshot } from "@/services/snapshots";
import type { SnapshotFileEntry } from "@/types/snapshot";

export interface SnapshotTreeBrowserProps {
  agentId: string;
  snapshotId: string;
  isAgentOnline: boolean;
  selectedPaths: string[];
  onSelectedPathsChange: (paths: string[]) => void;
}

interface TreeNode {
  name: string;
  path: string;
  type: "file" | "dir";
  size: number;
  mtime: string;
  children: TreeNode[] | null;
  loading: boolean;
  error?: string;
}

export function SnapshotTreeBrowser({
  agentId,
  snapshotId,
  isAgentOnline,
  selectedPaths,
  onSelectedPathsChange,
}: SnapshotTreeBrowserProps) {
  const [expanded, setExpanded] = useState(false);
  const [rootNodes, setRootNodes] = useState<TreeNode[]>([]);
  const [rootLoading, setRootLoading] = useState(false);
  const [rootError, setRootError] = useState<string | null>(null);
  const inflightRef = useRef<Set<string>>(new Set());
  const [expandedNodes, setExpandedNodes] = useState<Set<string>>(new Set());

  const selectedPathSet = useMemo(() => new Set(selectedPaths), [selectedPaths]);

  useEffect(() => {
    setExpanded(false);
    setRootNodes([]);
    setRootLoading(false);
    setRootError(null);
  }, [agentId, snapshotId]);

  const loadChildren = useCallback(
    async (path?: string): Promise<TreeNode[]> => {
      const resp = await browseSnapshot(agentId, {
        snapshot_id: snapshotId,
        ...(path ? { path } : {}),
      });
      if (resp.error) throw new Error(resp.error);
      return buildTreeNodes(resp.entries ?? [], path);
    },
    [agentId, snapshotId]
  );

  const handleExpand = useCallback(async () => {
    if (!isAgentOnline) return;
    setExpanded(true);
    setRootLoading(true);
    setRootError(null);
    try {
      const nodes = await loadChildren();
      setRootNodes(nodes);
    } catch (err: any) {
      setRootError(err?.message || "请求超时或 Agent 异常");
    } finally {
      setRootLoading(false);
    }
  }, [isAgentOnline, loadChildren]);

  const handleRefresh = useCallback(async () => {
    if (!isAgentOnline) return;
    setRootLoading(true);
    setRootError(null);
    try {
      const nodes = await loadChildren();
      setRootNodes(nodes);
    } catch (err: any) {
      setRootError(err?.message || "请求超时或 Agent 异常");
    } finally {
      setRootLoading(false);
    }
  }, [isAgentOnline, loadChildren]);

  const updateNodeAtPath = useCallback(
    (
      nodeList: TreeNode[],
      targetPath: string,
      updater: (n: TreeNode) => TreeNode
    ): TreeNode[] => {
      return nodeList.map((node) => {
        if (node.path === targetPath) return updater(node);
        if (node.children && targetPath.startsWith(node.path + "/")) {
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
      const node = findNode(rootNodes, path);
      if (!node || node.type !== "dir") return;
      const isExpanded = expandedNodes.has(path);
      if (isExpanded) {
        setExpandedNodes((prev) => {
          const next = new Set(prev);
          next.delete(path);
          return next;
        });
        return;
      }
      setExpandedNodes((prev) => {
        const next = new Set(prev);
        next.add(path);
        return next;
      });
      if (node.children !== null) return;
      if (inflightRef.current.has(path)) return;
      inflightRef.current.add(path);
      setRootNodes((prev) =>
        updateNodeAtPath(prev, path, (n) => ({ ...n, loading: true }))
      );
      loadChildren(path)
        .then((children) =>
          setRootNodes((prev) =>
            updateNodeAtPath(prev, path, (n) => ({
              ...n,
              children,
              loading: false,
              error: undefined,
            }))
          )
        )
        .catch((err: any) =>
          setRootNodes((prev) =>
            updateNodeAtPath(prev, path, (n) => ({
              ...n,
              children: [],
              loading: false,
              error: err?.message || "加载失败",
            }))
          )
        )
        .finally(() => inflightRef.current.delete(path));
    },
    [rootNodes, expandedNodes, loadChildren, updateNodeAtPath]
  );

  const handleCheck = useCallback(
    (node: TreeNode, checked: boolean) => {
      if (checked) {
        const next = [...selectedPaths];
        for (const path of collectLoadedPaths(node)) {
          if (!next.includes(path)) next.push(path);
        }
        onSelectedPathsChange(next);
        return;
      }
      const selectedAncestors = selectedPaths.filter((p) =>
        isAncestorPath(p, node.path)
      );
      const next = selectedPaths.filter((p) => {
        if (isSameOrDescendantPath(p, node.path)) return false;
        return !isAncestorPath(p, node.path);
      });
      for (const ancestorPath of selectedAncestors) {
        const ancestor = findNode(rootNodes, ancestorPath);
        if (!ancestor) continue;
        for (const path of collectLoadedPathsExcluding(ancestor, node.path)) {
          if (!next.includes(path)) next.push(path);
        }
      }
      onSelectedPathsChange(next);
    },
    [onSelectedPathsChange, rootNodes, selectedPaths]
  );

  if (!expanded) {
    return (
      <div style={{ border: "1px solid #f0f0f0", borderRadius: 6, padding: 12, background: "#fff" }}>
        <Button
          block
          icon={<SearchOutlined />}
          disabled={!isAgentOnline}
          onClick={handleExpand}
        >
          {isAgentOnline ? "浏览快照内容" : "需要节点在线才能浏览"}
        </Button>
      </div>
    );
  }

  return (
    <div
      style={{
        overflow: "hidden",
        border: "1px solid #f0f0f0",
        borderRadius: 6,
        background: "#fff",
      }}
    >
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          padding: "4px 12px",
          background: "#fafafa",
          borderBottom: "1px solid #f0f0f0",
        }}
      >
        <Typography.Text style={{ fontSize: 12, fontWeight: 500 }}>
          快照内容
        </Typography.Text>
        <Tooltip title="刷新">
          <Button
            type="text"
            size="small"
            aria-label="刷新快照内容"
            disabled={!isAgentOnline || rootLoading}
            onClick={handleRefresh}
            icon={<ReloadOutlined spin={rootLoading} />}
          />
        </Tooltip>
      </div>

      <div style={{ maxHeight: 350, overflowY: "auto" }}>
        {rootLoading ? (
          <div style={{ padding: 8 }}>
            {[1, 2, 3, 4, 5].map((i) => (
              <Skeleton.Input
                key={i}
                active
                size="small"
                block
                style={{ height: 28, marginBottom: 4 }}
              />
            ))}
            <Typography.Text
              type="secondary"
              style={{
                display: "block",
                textAlign: "center",
                fontSize: 12,
                paddingTop: 8,
              }}
            >
              正在读取快照内容...
            </Typography.Text>
          </div>
        ) : rootError ? (
          <div style={{ padding: 16 }}>
            <Alert
              type="error"
              showIcon
              message="无法读取快照内容"
              description={rootError}
            />
          </div>
        ) : rootNodes.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="快照为空"
            style={{ marginTop: 32 }}
          />
        ) : (
          <div style={{ padding: "4px 0" }}>
            {rootNodes.map((node) => (
              <TreeNodeRow
                key={node.path}
                node={node}
                depth={0}
                expandedNodes={expandedNodes}
                onToggle={handleToggle}
                selectedPathSet={selectedPathSet}
                onCheck={handleCheck}
              />
            ))}
          </div>
        )}
      </div>

      {selectedPaths.length > 0 && (
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            gap: 12,
            padding: "4px 12px",
            background: "#fafafa",
            borderTop: "1px solid #f0f0f0",
          }}
        >
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            已选中 {selectedPaths.length} 项
          </Typography.Text>
          <Button
            type="link"
            size="small"
            onClick={() => onSelectedPathsChange([])}
          >
            清除选择
          </Button>
        </div>
      )}
    </div>
  );
}

function TreeNodeRow({
  node,
  depth,
  expandedNodes,
  onToggle,
  selectedPathSet,
  onCheck,
}: {
  node: TreeNode;
  depth: number;
  expandedNodes: Set<string>;
  onToggle: (path: string) => void;
  selectedPathSet: Set<string>;
  onCheck: (node: TreeNode, checked: boolean) => void;
}) {
  const isDir = node.type === "dir";
  const isExpanded = expandedNodes.has(node.path);
  const checked = isNodeChecked(node, selectedPathSet);

  return (
    <>
      <div
        data-snapshot-tree-row=""
        style={{
          display: "flex",
          alignItems: "center",
          gap: 4,
          padding: "2px 8px",
          paddingLeft: depth * 16 + 8,
          minHeight: 32,
        }}
        onMouseOver={(e) => (e.currentTarget.style.background = "#fafafa")}
        onMouseOut={(e) => (e.currentTarget.style.background = "transparent")}
      >
        {isDir ? (
          <button
            type="button"
            onClick={() => onToggle(node.path)}
            aria-label={`${isExpanded ? "折叠" : "展开"} ${node.path}`}
            style={{
              background: "transparent",
              border: "none",
              padding: 0,
              cursor: "pointer",
              fontSize: 10,
              color: "rgba(0,0,0,0.45)",
              width: 16,
            }}
          >
            {node.loading ? (
              <ReloadOutlined spin style={{ fontSize: 10 }} />
            ) : isExpanded ? (
              <CaretDownOutlined />
            ) : (
              <RightOutlined />
            )}
          </button>
        ) : (
          <span style={{ width: 16 }} />
        )}

        <button
          type="button"
          onClick={() => isDir && onToggle(node.path)}
          style={{
            flex: 1,
            display: "flex",
            alignItems: "center",
            gap: 6,
            background: "transparent",
            border: "none",
            padding: 0,
            cursor: isDir ? "pointer" : "default",
            textAlign: "left",
            minWidth: 0,
            color: "inherit",
          }}
        >
          {isDir ? (
            isExpanded ? (
              <FolderOpenOutlined style={{ color: "#1677ff", fontSize: 14 }} />
            ) : (
              <FolderOutlined style={{ color: "#1677ff", fontSize: 14 }} />
            )
          ) : (
            <FileOutlined style={{ color: "rgba(0,0,0,0.45)", fontSize: 12 }} />
          )}
          <Typography.Text ellipsis style={{ fontSize: 12 }}>
            {node.name}
          </Typography.Text>
        </button>

        <Typography.Text
          type="secondary"
          style={{ fontSize: 11, width: 64, textAlign: "right" }}
        >
          {formatSize(node.size)}
        </Typography.Text>

        <Checkbox
          checked={checked}
          onChange={(e) => onCheck(node, e.target.checked)}
          aria-label={`选择 ${node.path}`}
          aria-checked={checked}
        />
      </div>

      {isDir &&
        isExpanded &&
        node.error &&
        (() => {
          const padLeft = (depth + 1) * 16 + 28;
          return (
            <div
              style={{
                padding: "2px 8px",
                paddingLeft: padLeft,
                fontSize: 10,
                color: "#c53030",
              }}
            >
              {node.error}
            </div>
          );
        })()}
      {isDir && isExpanded && node.children !== null && (
        <>
          {node.children.length === 0 && !node.loading && !node.error ? (
            <div
              style={{
                padding: "2px 8px",
                paddingLeft: (depth + 1) * 16 + 28,
                fontSize: 10,
                color: "rgba(0,0,0,0.45)",
              }}
            >
              空目录
            </div>
          ) : (
            node.children.map((child) => (
              <TreeNodeRow
                key={child.path}
                node={child}
                depth={depth + 1}
                expandedNodes={expandedNodes}
                onToggle={onToggle}
                selectedPathSet={selectedPathSet}
                onCheck={onCheck}
              />
            ))
          )}
        </>
      )}
    </>
  );
}

function sortEntries(entries: SnapshotFileEntry[]): SnapshotFileEntry[] {
  return [...entries].sort((a, b) => {
    if (a.type !== b.type) return a.type === "dir" ? -1 : 1;
    return a.path.localeCompare(b.path);
  });
}

function buildTreeNodes(
  entries: SnapshotFileEntry[],
  parentPath?: string
): TreeNode[] {
  const nodeMap = new Map<string, TreeNode>();
  const roots: TreeNode[] = [];
  const scoped = sortEntries(entries).filter((e) =>
    isInBrowseScope(e.path, parentPath)
  );
  for (const entry of scoped) {
    nodeMap.set(entry.path, {
      name: getPathName(entry.path),
      path: entry.path,
      type: entry.type,
      size: entry.size,
      mtime: entry.mtime,
      children: entry.type === "dir" ? null : [],
      loading: false,
    });
  }
  for (const entry of scoped) {
    const node = nodeMap.get(entry.path);
    if (!node) continue;
    const parent = nodeMap.get(getParentPath(entry.path) ?? "");
    if (parent) {
      parent.children = [...(parent.children ?? []), node];
      continue;
    }
    roots.push(node);
  }
  return sortTreeNodes(roots);
}

function sortTreeNodes(nodes: TreeNode[]): TreeNode[] {
  return [...nodes]
    .sort((a, b) => {
      if (a.type !== b.type) return a.type === "dir" ? -1 : 1;
      return a.path.localeCompare(b.path);
    })
    .map((n) => ({
      ...n,
      children: n.children ? sortTreeNodes(n.children) : n.children,
    }));
}

function isInBrowseScope(path: string, parentPath?: string): boolean {
  if (!parentPath) return path !== "/";
  return isDescendantPath(path, parentPath);
}

function getPathName(path: string): string {
  const parts = path.split("/").filter(Boolean);
  return parts[parts.length - 1] || path;
}

function getParentPath(path: string): string | null {
  const parts = path.split("/").filter(Boolean);
  if (parts.length <= 1) return null;
  const prefix = path.startsWith("/") ? "/" : "";
  return `${prefix}${parts.slice(0, -1).join("/")}`;
}

function collectLoadedPaths(node: TreeNode): string[] {
  return [
    node.path,
    ...(node.children?.flatMap((c) => collectLoadedPaths(c)) ?? []),
  ];
}

function collectLoadedPathsExcluding(node: TreeNode, excluded: string): string[] {
  if (isSameOrDescendantPath(node.path, excluded)) return [];
  if (isAncestorPath(node.path, excluded)) {
    return node.children?.flatMap((c) =>
      collectLoadedPathsExcluding(c, excluded)
    ) ?? [];
  }
  return collectLoadedPaths(node);
}

function isNodeChecked(node: TreeNode, set: Set<string>): boolean {
  if (set.has(node.path) || hasSelectedAncestor(node.path, set)) return true;
  if (node.type === "dir" && node.children && node.children.length > 0) {
    return node.children.every((c) => isNodeChecked(c, set));
  }
  return false;
}

function hasSelectedAncestor(path: string, set: Set<string>): boolean {
  for (const p of set) {
    if (isAncestorPath(p, path)) return true;
  }
  return false;
}

function isSameOrDescendantPath(path: string, parent: string): boolean {
  return path === parent || isDescendantPath(path, parent);
}

function isDescendantPath(path: string, parent: string): boolean {
  if (parent === "/") return path !== "/" && path.startsWith("/");
  const prefix = parent.endsWith("/") ? parent : `${parent}/`;
  return path.startsWith(prefix);
}

function isAncestorPath(candidate: string, path: string): boolean {
  if (candidate === path) return false;
  return isDescendantPath(path, candidate);
}

function findNode(nodes: TreeNode[], path: string): TreeNode | null {
  for (const node of nodes) {
    if (node.path === path) return node;
    if (node.children) {
      const found = findNode(node.children, path);
      if (found) return found;
    }
  }
  return null;
}

function formatSize(bytes: number): string {
  if (bytes === 0) return "";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}
