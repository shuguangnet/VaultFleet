import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Copy, Check } from "lucide-react";
import { copyToClipboard } from "@/lib/utils";

type ScriptSource = "github" | "github-proxy" | "master";

interface InstallCommandProps {
  enrollToken: string;
}

const GITHUB_RAW_URL =
  "https://raw.githubusercontent.com/shuguangnet/VaultFleet/main/internal/master/api/assets/install.sh";

export function InstallCommand({ enrollToken }: InstallCommandProps) {
  const [scriptSource, setScriptSource] = useState<ScriptSource>("github");
  const [masterHost, setMasterHost] = useState(window.location.origin);
  const [githubProxy, setGithubProxy] = useState("https://hk.gh-proxy.org/");
  const [copied, setCopied] = useState(false);

  const buildCommand = (): string => {
    switch (scriptSource) {
      case "github":
        return [
          `curl -fsSL ${GITHUB_RAW_URL} | bash -s -- \\`,
          `  --server ${masterHost} \\`,
          `  --token ${enrollToken}`,
        ].join("\n");

      case "github-proxy":
        return [
          `curl -fsSL ${githubProxy}${GITHUB_RAW_URL} | bash -s -- \\`,
          `  --server ${masterHost} \\`,
          `  --token ${enrollToken} \\`,
          `  --github-proxy ${githubProxy}`,
        ].join("\n");

      case "master":
        return [
          `curl -fsSL ${masterHost}/install.sh | bash -s -- \\`,
          `  --server ${masterHost} \\`,
          `  --token ${enrollToken}`,
        ].join("\n");
    }
  };

  const command = buildCommand();

  const handleCopy = () => {
    copyToClipboard(command).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };

  return (
    <div className="space-y-4">
      {/* Script source selection */}
      <div className="space-y-2">
        <Label>脚本来源</Label>
        <div className="space-y-2">
          {(
            [
              { value: "github", label: "GitHub（推荐）", desc: "直接从 GitHub 下载" },
              { value: "github-proxy", label: "GitHub + 代理", desc: "通过代理加速，适合国内网络" },
              { value: "master", label: "Master 服务器", desc: "从 Master 下载脚本" },
            ] as const
          ).map((option) => (
            <label
              key={option.value}
              className={`flex items-start gap-3 rounded-md border p-3 cursor-pointer transition-colors ${
                scriptSource === option.value
                  ? "border-primary bg-primary/5"
                  : "border-border hover:bg-muted/50"
              }`}
            >
              <input
                type="radio"
                name="script-source"
                value={option.value}
                checked={scriptSource === option.value}
                onChange={() => setScriptSource(option.value)}
                className="mt-0.5 accent-primary"
              />
              <div className="flex flex-col">
                <span className="text-sm font-medium">{option.label}</span>
                <span className="text-xs text-muted-foreground">{option.desc}</span>
              </div>
            </label>
          ))}
        </div>
      </div>

      {/* GitHub proxy input (only for github-proxy mode) */}
      {scriptSource === "github-proxy" && (
        <div className="space-y-2">
          <Label htmlFor="github-proxy">GitHub 代理地址</Label>
          <Input
            id="github-proxy"
            value={githubProxy}
            onChange={(e) => setGithubProxy(e.target.value)}
            placeholder="https://hk.gh-proxy.org/"
          />
        </div>
      )}

      {/* Master host input */}
      <div className="space-y-2">
        <Label htmlFor="master-host">Master 地址</Label>
        <Input
          id="master-host"
          value={masterHost}
          onChange={(e) => setMasterHost(e.target.value)}
          placeholder="http://master-ip:8080"
        />
        <p className="text-xs text-muted-foreground">
          Agent 将连接此地址与 Master 通信
        </p>
      </div>

      {/* Generated install command */}
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
            onClick={handleCopy}
          >
            {copied ? <Check className="h-4 w-4 text-green-500" /> : <Copy className="h-4 w-4" />}
          </Button>
        </div>
      </div>
    </div>
  );
}
