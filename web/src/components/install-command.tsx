import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Copy, Check } from "lucide-react";

interface InstallCommandProps {
  enrollToken: string;
}

export function InstallCommand({ enrollToken }: InstallCommandProps) {
  const [masterHost, setMasterHost] = useState(window.location.origin);
  const [copied, setCopied] = useState(false);

  const command = `curl -fsSL ${masterHost}/install.sh | bash -s -- \\
  --server ${masterHost} \\
  --token ${enrollToken}`;

  const copyToClipboard = () => {
    navigator.clipboard.writeText(command);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="master-host">Master 地址</Label>
        <div className="flex gap-2">
          <Input
            id="master-host"
            value={masterHost}
            onChange={(e) => setMasterHost(e.target.value)}
            placeholder="http://master-ip:8080"
          />
        </div>
        <p className="text-xs text-muted-foreground">
          如果 Agent 无法通过当前浏览器 URL 访问 Master，请修改此地址。
        </p>
      </div>

      <div className="space-y-2">
        <Label>安装指令</Label>
        <div className="relative">
          <pre className="bg-muted p-4 rounded-lg text-xs overflow-x-auto whitespace-pre-wrap break-all font-mono leading-relaxed">
            {command}
          </pre>
          <Button
            size="icon"
            variant="ghost"
            className="absolute top-2 right-2 h-8 w-8"
            onClick={copyToClipboard}
          >
            {copied ? <Check className="h-4 w-4 text-green-500" /> : <Copy className="h-4 w-4" />}
          </Button>
        </div>
      </div>
    </div>
  );
}
