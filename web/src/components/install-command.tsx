import { useState } from "react";
import {
  Button,
  Input,
  Typography,
} from "antd";
import { CheckOutlined, CopyOutlined } from "@ant-design/icons";
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

  const sources: { value: ScriptSource; label: string; desc: string }[] = [
    { value: "github", label: "GitHub（推荐）", desc: "直接从 GitHub 下载" },
    {
      value: "github-proxy",
      label: "GitHub + 代理",
      desc: "通过代理加速，适合国内网络",
    },
    { value: "master", label: "Master 服务器", desc: "从 Master 下载脚本" },
  ];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div>
        <Typography.Text strong>脚本来源</Typography.Text>
        <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 8 }}>
          {sources.map((option) => (
            <label
              key={option.value}
              style={{
                display: "flex",
                gap: 10,
                alignItems: "flex-start",
                padding: "10px 12px",
                borderRadius: 6,
                border: `1px solid ${
                  scriptSource === option.value ? "#1f4f8f" : "#f0f0f0"
                }`,
                background:
                  scriptSource === option.value ? "rgba(22,104,220,0.04)" : "transparent",
                cursor: "pointer",
              }}
            >
              <input
                type="radio"
                name="script-source"
                value={option.value}
                checked={scriptSource === option.value}
                onChange={() => setScriptSource(option.value)}
                style={{ marginTop: 2, accentColor: "#1f4f8f" }}
              />
              <span>
                <div style={{ fontSize: 13, fontWeight: 500 }}>{option.label}</div>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {option.desc}
                </Typography.Text>
              </span>
            </label>
          ))}
        </div>
      </div>

      {scriptSource === "github-proxy" && (
        <div>
          <Typography.Text strong>GitHub 代理地址</Typography.Text>
          <Input
            value={githubProxy}
            onChange={(e) => setGithubProxy(e.target.value)}
            placeholder="https://hk.gh-proxy.org/"
            style={{ marginTop: 6 }}
          />
        </div>
      )}

      <div>
        <Typography.Text strong>Master 地址</Typography.Text>
        <Input
          value={masterHost}
          onChange={(e) => setMasterHost(e.target.value)}
          placeholder="http://master-ip:8080"
          style={{ marginTop: 6 }}
        />
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          Agent 将连接此地址与 Master 通信
        </Typography.Text>
      </div>

      <div>
        <Typography.Text strong>安装指令</Typography.Text>
        <div style={{ position: "relative", marginTop: 6 }}>
          <pre
            style={{
              background: "#f5f7fa",
              padding: 16,
              paddingRight: 48,
              borderRadius: 6,
              fontSize: 12,
              lineHeight: 1.6,
              margin: 0,
              whiteSpace: "pre-wrap",
              wordBreak: "break-all",
              fontFamily:
                "JetBrains Mono, Fira Code, SFMono-Regular, Menlo, Consolas, monospace",
              border: "1px solid #f0f0f0",
            }}
          >
            {command}
          </pre>
          <Button
            type="text"
            icon={copied ? <CheckOutlined style={{ color: "#2f855a" }} /> : <CopyOutlined />}
            onClick={handleCopy}
            className="vf-icon-button"
            style={{ position: "absolute", top: 8, right: 8 }}
          />
        </div>
      </div>
    </div>
  );
}
