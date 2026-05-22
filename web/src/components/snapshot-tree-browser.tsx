import { useCallback, useEffect, useMemo, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import {
  AlertCircle,
  ChevronDown,
  ChevronRight,
  FileText,
  Folder,
  FolderOpen,
  FolderSearch,
  RefreshCw,
} from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { browseSnapshot } from "@/services/snapshots";
import { SnapshotFileEntry } from "@/types/snapshot";

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
  children: TreeNode[];
}

export function SnapshotTreeBrowser({
  agentId,
  snapshotId,
  isAgentOnline,
  selectedPaths,
  onSelectedPathsChange,
}: SnapshotTreeBrowserProps) {
  const [expanded, setExpanded] = useState(false);
  const [expandedNodes, setExpandedNodes] = useState<Set<string>>(new Set());

  const browseMutation = useMutation({
    mutationFn: () => browseSnapshot(agentId, { snapshot_id: snapshotId }),
  });

  useEffect(() => {
    setExpanded(false);
    setExpandedNodes(new Set());
    browseMutation.reset();
  }, [agentId, snapshotId]);

  const tree = useMemo(() => {
    return buildTree(browseMutation.data?.entries ?? []);
  }, [browseMutation.data?.entries]);

  const selectedPathSet = useMemo(() => new Set(selectedPaths), [selectedPaths]);

  const handleExpand = useCallback(() => {
    if (!isAgentOnline) {
      return;
    }
    setExpanded(true);
    if (!browseMutation.data && !browseMutation.isPending) {
      browseMutation.mutate();
    }
  }, [browseMutation, isAgentOnline]);

  const handleRefresh = useCallback(() => {
    if (!isAgentOnline) {
      return;
    }
    browseMutation.mutate();
  }, [browseMutation, isAgentOnline]);

  const handleToggleNode = useCallback((path: string) => {
    setExpandedNodes((prev) => {
      const next = new Set(prev);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  }, []);

  const getCheckedState = useCallback(
    (node: TreeNode): boolean | "indeterminate" => {
      const affectedPaths = node.type === "dir" && node.children.length > 0
        ? getDescendantPaths(node)
        : [node.path];
      const selectedCount = affectedPaths.filter((path) => selectedPathSet.has(path)).length;

      if (selectedCount === 0) {
        return false;
      }
      if (selectedCount === affectedPaths.length) {
        return true;
      }
      return "indeterminate";
    },
    [selectedPathSet],
  );

  const handleCheck = useCallback(
    (node: TreeNode, checked: boolean) => {
      const affectedPaths = getAllPaths(node);
      if (checked) {
        const next = [...selectedPaths];
        for (const path of affectedPaths) {
          if (!next.includes(path)) {
            next.push(path);
          }
        }
        onSelectedPathsChange(next);
        return;
      }

      const affectedPathSet = new Set(affectedPaths);
      onSelectedPathsChange(selectedPaths.filter((path) => {
        if (affectedPathSet.has(path)) {
          return false;
        }
        return !isAncestorPath(path, node.path);
      }));
    },
    [onSelectedPathsChange, selectedPaths],
  );

  if (!expanded) {
    return (
      <div className="rounded-md border bg-card p-3">
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-full"
          disabled={!isAgentOnline}
          onClick={handleExpand}
        >
          <FolderSearch className="h-4 w-4" />
          {isAgentOnline ? "浏览快照内容" : "需要节点在线才能浏览"}
        </Button>
      </div>
    );
  }

  const errorMessage = browseMutation.isError
    ? getErrorMessage(browseMutation.error)
    : browseMutation.data?.error;

  return (
    <div className="overflow-hidden rounded-md border bg-card">
      <div className="flex items-center justify-between gap-2 border-b bg-muted/50 px-3 py-2">
        <span className="min-w-0 truncate text-xs font-medium">快照内容</span>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="h-7 w-7 shrink-0"
          disabled={!isAgentOnline || browseMutation.isPending}
          onClick={handleRefresh}
          aria-label="刷新快照内容"
        >
          <RefreshCw className={cn("h-3.5 w-3.5", browseMutation.isPending && "animate-spin")} />
        </Button>
      </div>

      <div className="max-h-[350px] overflow-y-auto">
        {browseMutation.isPending ? (
          <LoadingRows />
        ) : errorMessage ? (
          <div className="p-4">
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertTitle>无法读取快照内容</AlertTitle>
              <AlertDescription className="text-xs">
                {errorMessage || "请求超时或 Agent 异常"}
              </AlertDescription>
            </Alert>
          </div>
        ) : tree.length === 0 ? (
          <div className="p-8 text-center text-sm text-muted-foreground">快照为空</div>
        ) : (
          <div className="py-1">
            {tree.map((node) => (
              <TreeNodeRow
                key={node.path}
                node={node}
                depth={0}
                expandedNodes={expandedNodes}
                onToggle={handleToggleNode}
                checkedStateFor={getCheckedState}
                onCheck={handleCheck}
              />
            ))}
          </div>
        )}
      </div>

      {selectedPaths.length > 0 && (
        <div className="flex items-center justify-between gap-3 border-t bg-muted/30 px-3 py-2">
          <span className="min-w-0 truncate text-xs text-muted-foreground">
            已选中 {selectedPaths.length} 项
          </span>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 shrink-0 px-2 text-xs"
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
  checkedStateFor,
  onCheck,
}: {
  node: TreeNode;
  depth: number;
  expandedNodes: Set<string>;
  onToggle: (path: string) => void;
  checkedStateFor: (node: TreeNode) => boolean | "indeterminate";
  onCheck: (node: TreeNode, checked: boolean) => void;
}) {
  const isDir = node.type === "dir";
  const isExpanded = expandedNodes.has(node.path);

  return (
    <>
      <div
        className="group flex min-h-8 items-center gap-1 px-2 py-1 hover:bg-muted/50"
        data-snapshot-tree-row=""
        style={{ paddingLeft: `${depth * 16 + 8}px` }}
      >
        {isDir ? (
          <button
            type="button"
            className="flex h-5 w-5 shrink-0 items-center justify-center rounded text-muted-foreground hover:text-foreground"
            onClick={() => onToggle(node.path)}
            aria-label={`${isExpanded ? "折叠" : "展开"} ${node.path}`}
          >
            {isExpanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          </button>
        ) : (
          <span className="h-5 w-5 shrink-0" aria-hidden="true" />
        )}

        <button
          type="button"
          className={cn(
            "flex min-w-0 flex-1 items-center gap-1.5 text-left",
            isDir ? "cursor-pointer" : "cursor-default",
          )}
          onClick={() => {
            if (isDir) {
              onToggle(node.path);
            }
          }}
          disabled={!isDir}
        >
          {isDir ? (
            isExpanded ? (
              <FolderOpen className="h-4 w-4 shrink-0 fill-blue-500/20 text-blue-500" />
            ) : (
              <Folder className="h-4 w-4 shrink-0 fill-blue-500/20 text-blue-500" />
            )
          ) : (
            <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          )}
          <span className="min-w-0 truncate text-xs text-foreground">{node.name}</span>
        </button>

        <span className="w-16 shrink-0 truncate text-right text-[10px] text-muted-foreground">
          {formatSize(node.size)}
        </span>

        <Checkbox
          checked={checkedStateFor(node)}
          onCheckedChange={(value) => onCheck(node, value === true)}
          className="shrink-0"
          aria-label={`选择 ${node.path}`}
        />
      </div>

      {isDir && isExpanded && (
        <>
          {node.children.length === 0 ? (
            <div
              className="px-2 py-1 text-[10px] text-muted-foreground"
              style={{ paddingLeft: `${(depth + 1) * 16 + 28}px` }}
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
                checkedStateFor={checkedStateFor}
                onCheck={onCheck}
              />
            ))
          )}
        </>
      )}
    </>
  );
}

function LoadingRows() {
  return (
    <div className="space-y-2 p-2">
      {[1, 2, 3, 4, 5].map((i) => (
        <Skeleton key={i} className="h-7 w-full" />
      ))}
      <p className="pt-2 text-center text-xs text-muted-foreground">正在读取快照内容...</p>
    </div>
  );
}

function buildTree(entries: SnapshotFileEntry[]): TreeNode[] {
  const roots: TreeNode[] = [];
  const nodeMap = new Map<string, TreeNode>();

  for (const entry of [...entries].sort((a, b) => a.path.localeCompare(b.path))) {
    const node: TreeNode = {
      name: getPathName(entry.path),
      path: entry.path,
      type: entry.type,
      size: entry.size,
      mtime: entry.mtime,
      children: [],
    };
    nodeMap.set(entry.path, node);

    const parentPath = getParentPath(entry.path);
    const parent = parentPath ? nodeMap.get(parentPath) : undefined;
    if (parent) {
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  }

  return roots;
}

function getAllPaths(node: TreeNode): string[] {
  return [node.path, ...node.children.flatMap((child) => getAllPaths(child))];
}

function getDescendantPaths(node: TreeNode): string[] {
  return node.children.flatMap((child) => getAllPaths(child));
}

function getPathName(path: string): string {
  const parts = path.split("/").filter(Boolean);
  return parts[parts.length - 1] || path;
}

function getParentPath(path: string): string | null {
  const parts = path.split("/").filter(Boolean);
  if (parts.length <= 1) {
    return null;
  }
  const prefix = path.startsWith("/") ? "/" : "";
  return `${prefix}${parts.slice(0, -1).join("/")}`;
}

function isAncestorPath(candidate: string, path: string): boolean {
  if (candidate === path) {
    return false;
  }
  if (candidate === "/") {
    return path !== "/";
  }
  return path.startsWith(candidate.endsWith("/") ? candidate : `${candidate}/`);
}

function formatSize(bytes: number): string {
  if (bytes === 0) {
    return "";
  }
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  if (bytes < 1024 * 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

function getErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message || "请求超时或 Agent 异常";
  }
  return "请求超时或 Agent 异常";
}
