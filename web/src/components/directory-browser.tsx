import { useState, useCallback } from "react";
import { useMutation } from "@tanstack/react-query";
import { browseAgent } from "@/services/agents";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Folder, FolderOpen, ChevronRight, ChevronDown, Home, RefreshCw, AlertCircle } from "lucide-react";
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

export function DirectoryBrowser({ agentId, onSelect, onDeselect, selectedPaths = [], className }: DirectoryBrowserProps) {
  const [nodes, setNodes] = useState<TreeNode[]>([]);
  const [rootLoading, setRootLoading] = useState(true);
  const [rootError, setRootError] = useState<string | null>(null);

  const fetchChildren = useCallback(async (path: string): Promise<BrowseEntry[]> => {
    const resp = await browseAgent(agentId, { path, depth: 1 });
    return resp.entries || [];
  }, [agentId]);

  const loadRoot = useCallback(async () => {
    setRootLoading(true);
    setRootError(null);
    try {
      const entries = await fetchChildren("/");
      setNodes(entries
        .filter(e => e.type === "dir")
        .map(e => ({ entry: e, children: null, loading: false, expanded: false }))
      );
    } catch (err: any) {
      setRootError(err?.message || "加载失败");
    } finally {
      setRootLoading(false);
    }
  }, [fetchChildren]);

  useState(() => { loadRoot(); });

  const updateNodeAtPath = useCallback((
    nodeList: TreeNode[],
    targetPath: string,
    updater: (node: TreeNode) => TreeNode
  ): TreeNode[] => {
    return nodeList.map(node => {
      if (node.entry.path === targetPath) {
        return updater(node);
      }
      if (node.children && targetPath.startsWith(node.entry.path + "/")) {
        return { ...node, children: updateNodeAtPath(node.children, targetPath, updater) };
      }
      return node;
    });
  }, []);

  const handleToggle = useCallback(async (path: string) => {
    let needsFetch = false;

    setNodes(prev => {
      const node = findNode(prev, path);
      if (!node) return prev;

      if (node.expanded) {
        return updateNodeAtPath(prev, path, n => ({ ...n, expanded: false }));
      }

      if (node.children !== null) {
        return updateNodeAtPath(prev, path, n => ({ ...n, expanded: true }));
      }

      needsFetch = true;
      return updateNodeAtPath(prev, path, n => ({ ...n, loading: true, expanded: true }));
    });

    if (needsFetch) {
      try {
        const entries = await fetchChildren(path);
        const children = entries
          .filter(e => e.type === "dir")
          .map(e => ({ entry: e, children: null, loading: false, expanded: false } as TreeNode));
        setNodes(prev => updateNodeAtPath(prev, path, n => ({ ...n, children, loading: false })));
      } catch (err: any) {
        setNodes(prev => updateNodeAtPath(prev, path, n => ({
          ...n, loading: false, error: err?.message || "加载失败"
        })));
      }
    }
  }, [fetchChildren, updateNodeAtPath]);

  const handleCheck = useCallback((path: string, checked: boolean) => {
    if (checked) {
      onSelect(path);
    } else {
      onDeselect?.(path);
    }
  }, [onSelect, onDeselect]);

  const isSelected = useCallback((path: string) => {
    return selectedPaths.includes(path);
  }, [selectedPaths]);

  return (
    <div className={cn("border rounded-md bg-card overflow-hidden", className)}>
      <div className="bg-muted/50 p-2 flex items-center gap-2 border-b">
        <Button variant="ghost" size="icon" className="h-8 w-8" onClick={loadRoot} disabled={rootLoading}>
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
          <RefreshCw className={cn("h-4 w-4", rootLoading && "animate-spin")} />
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
          <div className="p-8 text-center text-muted-foreground text-sm">目录为空</div>
        ) : (
          <div className="py-1">
            {nodes.map(node => (
              <TreeNodeRow
                key={node.entry.path}
                node={node}
                depth={0}
                isSelected={isSelected}
                onToggle={handleToggle}
                onCheck={handleCheck}
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
}: {
  node: TreeNode;
  depth: number;
  isSelected: (path: string) => boolean;
  onToggle: (path: string) => void;
  onCheck: (path: string, checked: boolean) => void;
}) {
  const checked = isSelected(node.entry.path);
  const name = node.entry.path.split("/").filter(Boolean).pop() || node.entry.path;

  return (
    <>
      <div
        className="flex items-center gap-1 px-2 py-1.5 hover:bg-muted/50 group"
        style={{ paddingLeft: `${depth * 20 + 8}px` }}
      >
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

        <div
          className="flex items-center gap-1.5 flex-1 min-w-0 cursor-pointer"
          onClick={() => onToggle(node.entry.path)}
        >
          {node.expanded ? (
            <FolderOpen className="h-4 w-4 text-blue-500 fill-blue-500/20 shrink-0" />
          ) : (
            <Folder className="h-4 w-4 text-blue-500 fill-blue-500/20 shrink-0" />
          )}
          <span className="text-sm truncate select-none">{name}</span>
        </div>

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

      {node.expanded && node.children && node.children.length === 0 && !node.loading && (
        <div
          className="text-xs text-muted-foreground px-2 py-1"
          style={{ paddingLeft: `${(depth + 1) * 20 + 8}px` }}
        >
          无子目录
        </div>
      )}

      {node.expanded && node.children?.map(child => (
        <TreeNodeRow
          key={child.entry.path}
          node={child}
          depth={depth + 1}
          isSelected={isSelected}
          onToggle={onToggle}
          onCheck={onCheck}
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
