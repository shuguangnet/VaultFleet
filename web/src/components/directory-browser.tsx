import { useState, useCallback, useRef } from "react";
import { browseAgent, dirSizeAgent } from "@/services/agents";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Folder,
  FolderOpen,
  FileText,
  ChevronRight,
  ChevronDown,
  Home,
  RefreshCw,
  AlertCircle,
  Ruler,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { BrowseEntry } from "@/types/api";

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
    if (a.type !== b.type) {
      return a.type === "dir" ? -1 : 1;
    }
    return a.path.localeCompare(b.path);
  });
}

function formatSize(bytes: number): string {
  if (bytes < 0) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024)
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
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
  className,
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

  useState(() => {
    loadRoot();
  });

  const updateNodeAtPath = useCallback(
    (
      nodeList: TreeNode[],
      targetPath: string,
      updater: (node: TreeNode) => TreeNode
    ): TreeNode[] => {
      return nodeList.map((node) => {
        if (node.entry.path === targetPath) {
          return updater(node);
        }
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
          return updateNodeAtPath(prev, path, (n) => ({
            ...n,
            expanded: false,
          }));
        }

        if (node.children !== null) {
          return updateNodeAtPath(prev, path, (n) => ({
            ...n,
            expanded: true,
          }));
        }

        if (inflightRef.current.has(path)) {
          return updateNodeAtPath(prev, path, (n) => ({
            ...n,
            expanded: true,
          }));
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
          .finally(() => {
            inflightRef.current.delete(path);
          });

        return updateNodeAtPath(prev, path, (n) => ({
          ...n,
          loading: true,
          expanded: true,
        }));
      });
    },
    [fetchChildren, updateNodeAtPath]
  );

  const handleCheck = useCallback(
    (path: string, checked: boolean) => {
      if (checked) {
        onSelect(path);
      } else {
        onDeselect?.(path);
      }
    },
    [onSelect, onDeselect]
  );

  const isSelected = useCallback(
    (path: string) => {
      return selectedPaths.includes(path);
    },
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
        if (resp.size > 0) {
          setDirSizes((prev) => {
            const next = new Map(prev);
            next.set(path, resp.size);
            return next;
          });
        } else if (resp.error) {
          setDirSizes((prev) => {
            const next = new Map(prev);
            next.set(path, "error");
            return next;
          });
        } else {
          setDirSizes((prev) => {
            const next = new Map(prev);
            next.set(path, 0);
            return next;
          });
        }
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
    <div
      className={cn("border rounded-md bg-card overflow-hidden", className)}
    >
      <div className="bg-muted/50 p-2 flex items-center gap-2 border-b">
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8"
          onClick={loadRoot}
          disabled={rootLoading}
        >
          <Home className="h-4 w-4" />
        </Button>
        <div className="flex-1 text-xs font-mono truncate px-2 py-1 bg-background border rounded">
          /
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8"
          onClick={loadRoot}
          disabled={rootLoading}
        >
          <RefreshCw
            className={cn("h-4 w-4", rootLoading && "animate-spin")}
          />
        </Button>
      </div>

      <div className="h-[300px] overflow-y-auto">
        {rootError ? (
          <div className="p-4">
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertTitle>无法浏览目录</AlertTitle>
              <AlertDescription className="text-xs">
                {rootError}
              </AlertDescription>
            </Alert>
          </div>
        ) : rootLoading ? (
          <div className="p-2 space-y-2">
            {[1, 2, 3, 4, 5].map((i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : nodes.length === 0 ? (
          <div className="p-8 text-center text-muted-foreground text-sm">
            目录为空
          </div>
        ) : (
          <div className="py-1">
            {nodes.map((node) => (
              <TreeNodeRow
                key={node.entry.path}
                node={node}
                depth={0}
                isSelected={isSelected}
                onToggle={handleToggle}
                onCheck={handleCheck}
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
        className="flex items-center gap-1 px-2 py-1.5 hover:bg-muted/50 group"
        style={{ paddingLeft: `${depth * 20 + 8}px` }}
      >
        {isDir ? (
          <button
            type="button"
            className="h-5 w-5 flex items-center justify-center shrink-0 text-muted-foreground hover:text-foreground rounded"
            onClick={() => onToggle(node.entry.path)}
          >
            {node.loading ? (
              <RefreshCw className="h-3.5 w-3.5 animate-spin" />
            ) : node.expanded ? (
              <ChevronDown className="h-3.5 w-3.5" />
            ) : (
              <ChevronRight className="h-3.5 w-3.5" />
            )}
          </button>
        ) : (
          <span className="h-5 w-5 shrink-0" />
        )}

        <div
          className={cn(
            "flex items-center gap-1.5 flex-1 min-w-0",
            isDir && "cursor-pointer"
          )}
          onClick={isDir ? () => onToggle(node.entry.path) : undefined}
        >
          {isDir ? (
            node.expanded ? (
              <FolderOpen className="h-4 w-4 text-blue-500 fill-blue-500/20 shrink-0" />
            ) : (
              <Folder className="h-4 w-4 text-blue-500 fill-blue-500/20 shrink-0" />
            )
          ) : (
            <FileText className="h-4 w-4 text-muted-foreground shrink-0" />
          )}
          <span className="text-sm truncate select-none">{name}</span>
        </div>

        {!isDir && node.entry.size > 0 && (
          <span className="text-xs text-muted-foreground shrink-0 tabular-nums">
            {formatSize(node.entry.size)}
          </span>
        )}

        {isDir && dirSize === undefined && (
          <button
            type="button"
            className="h-5 w-5 flex items-center justify-center shrink-0 text-muted-foreground hover:text-foreground rounded opacity-0 group-hover:opacity-100 transition-opacity"
            onClick={(e) => {
              e.stopPropagation();
              onCalcSize(node.entry.path);
            }}
            title="计算目录大小"
          >
            <Ruler className="h-3.5 w-3.5" />
          </button>
        )}
        {isDir && dirSize === "loading" && (
          <RefreshCw className="h-3.5 w-3.5 animate-spin text-muted-foreground shrink-0" />
        )}
        {isDir && dirSize === "error" && (
          <span className="text-xs text-destructive shrink-0">错误</span>
        )}
        {isDir && typeof dirSize === "number" && (
          <span className="text-xs text-muted-foreground shrink-0 tabular-nums">
            {formatSize(dirSize)}
          </span>
        )}

        <Checkbox
          checked={checked}
          onCheckedChange={(val) => onCheck(node.entry.path, !!val)}
          className="shrink-0"
        />
      </div>

      {node.expanded && node.error && (
        <div
          className="text-xs text-destructive px-2 py-1"
          style={{ paddingLeft: `${(depth + 1) * 20 + 8}px` }}
        >
          {node.error}
        </div>
      )}

      {node.expanded &&
        node.children &&
        node.children.length === 0 &&
        !node.loading && (
          <div
            className="text-xs text-muted-foreground px-2 py-1"
            style={{ paddingLeft: `${(depth + 1) * 20 + 8}px` }}
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
