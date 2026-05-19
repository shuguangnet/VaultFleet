import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { browseAgent } from "@/services/agents";
import { Button } from "@/components/ui/button";
import { Folder, File, ChevronRight, Home, RefreshCw, AlertCircle } from "lucide-react";
import { cn } from "@/lib/utils";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

interface DirectoryBrowserProps {
  agentId: string;
  onSelect: (path: string) => void;
  className?: string;
}

export function DirectoryBrowser({ agentId, onSelect, className }: DirectoryBrowserProps) {
  const [currentPath, setCurrentPath] = useState("/");
  
  const browseMutation = useMutation({
    mutationFn: (path: string) => browseAgent(agentId, { path, depth: 1 }),
  });

  // Initial load
  useState(() => {
    browseMutation.mutate("/");
  });

  const handleNavigate = (path: string) => {
    setCurrentPath(path);
    browseMutation.mutate(path);
  };

  const handleParent = () => {
    if (currentPath === "/") return;
    const parts = currentPath.split("/").filter(Boolean);
    parts.pop();
    const parentPath = "/" + parts.join("/");
    handleNavigate(parentPath);
  };

  const entries = browseMutation.data?.entries || [];

  return (
    <div className={cn("border rounded-md bg-card overflow-hidden", className)}>
      <div className="bg-muted/50 p-2 flex items-center gap-2 border-b">
        <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => handleNavigate("/")}>
          <Home className="h-4 w-4" />
        </Button>
        <div className="flex-1 text-xs font-mono truncate px-2 py-1 bg-background border rounded">
          {currentPath}
        </div>
        <Button 
          variant="ghost" 
          size="icon" 
          className="h-8 w-8" 
          onClick={() => browseMutation.mutate(currentPath)}
          disabled={browseMutation.isPending}
        >
          <RefreshCw className={cn("h-4 w-4", browseMutation.isPending && "animate-spin")} />
        </Button>
      </div>

      <div className="h-[300px] overflow-y-auto">
        {browseMutation.isError ? (
          <div className="p-4">
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertTitle>无法浏览目录</AlertTitle>
              <AlertDescription className="text-xs">
                {(browseMutation.error as any)?.message || "Agent 响应超时或权限不足。"}
              </AlertDescription>
            </Alert>
          </div>
        ) : browseMutation.isPending ? (
          <div className="p-2 space-y-2">
            {[1, 2, 3, 4, 5].map((i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : (
          <div className="divide-y">
            {currentPath !== "/" && (
              <div 
                className="flex items-center gap-2 px-3 py-2 hover:bg-muted/50 cursor-pointer text-sm font-medium"
                onClick={handleParent}
              >
                <Folder className="h-4 w-4 text-blue-500 fill-blue-500/20" />
                <span>..</span>
              </div>
            )}
            {entries.length === 0 ? (
              <div className="p-8 text-center text-muted-foreground text-sm">目录为空</div>
            ) : (
              entries.map((entry) => (
                <div 
                  key={entry.path}
                  className="flex items-center justify-between px-3 py-2 hover:bg-muted/50 group cursor-default"
                >
                  <div 
                    className="flex items-center gap-2 flex-1 cursor-pointer min-w-0"
                    onClick={() => entry.type === "dir" ? handleNavigate(entry.path) : null}
                  >
                    {entry.type === "dir" ? (
                      <Folder className="h-4 w-4 text-blue-500 fill-blue-500/20 shrink-0" />
                    ) : (
                      <File className="h-4 w-4 text-muted-foreground shrink-0" />
                    )}
                    <span className="text-sm truncate">{entry.path.split("/").pop()}</span>
                  </div>
                  <Button 
                    variant="ghost" 
                    size="sm" 
                    className="h-7 text-xs opacity-0 group-hover:opacity-100 transition-opacity px-2"
                    onClick={() => onSelect(entry.path)}
                  >
                    选择
                  </Button>
                </div>
              ))
            )}
          </div>
        )}
      </div>
    </div>
  );
}
