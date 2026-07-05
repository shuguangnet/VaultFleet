import { Button, Input, Space } from "antd";
import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";

interface KeyValueEditorProps {
  value: Record<string, string>;
  onChange: (value: Record<string, string>) => void;
}

export function KeyValueEditor({ value, onChange }: KeyValueEditorProps) {
  const entries = Object.entries(value);

  const handleKeyChange = (oldKey: string, newKey: string) => {
    if (oldKey === newKey) return;
    const newValue = { ...value };
    const val = newValue[oldKey];
    delete newValue[oldKey];
    newValue[newKey] = val;
    onChange(newValue);
  };

  const handleValueChange = (key: string, newVal: string) => {
    onChange({ ...value, [key]: newVal });
  };

  const handleRemove = (key: string) => {
    const newValue = { ...value };
    delete newValue[key];
    onChange(newValue);
  };

  const handleAdd = () => {
    let key = "new_key";
    let i = 1;
    while (key in value) {
      key = `new_key_${i++}`;
    }
    onChange({ ...value, [key]: "" });
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      {entries.map(([k, v]) => (
        <Space.Compact key={k} className="vf-key-value-row" style={{ width: "100%" }}>
          <Input
            value={k}
            onChange={(e) => handleKeyChange(k, e.target.value)}
            placeholder="Key"
            style={{ flex: 1, fontFamily: "monospace", fontSize: 12 }}
          />
          <Input
            value={v}
            onChange={(e) => handleValueChange(k, e.target.value)}
            placeholder="Value"
            style={{ flex: 2, fontFamily: "monospace", fontSize: 12 }}
            type={
              k.includes("password") ||
              k.includes("secret") ||
              k.includes("key")
                ? "password"
                : "text"
            }
          />
          <Button
            icon={<DeleteOutlined />}
            danger
            onClick={() => handleRemove(k)}
          />
        </Space.Compact>
      ))}
      <Button
        type="dashed"
        icon={<PlusOutlined />}
        onClick={handleAdd}
        block
      >
        添加配置项
      </Button>
    </div>
  );
}
