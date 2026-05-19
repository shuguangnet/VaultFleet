import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Plus, Trash2 } from "lucide-react";

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
    <div className="space-y-2">
      {entries.map(([k, v]) => (
        <div key={k} className="flex gap-2 items-center">
          <Input
            value={k}
            onChange={(e) => handleKeyChange(k, e.target.value)}
            placeholder="Key"
            className="flex-1 font-mono text-xs"
          />
          <Input
            value={v}
            onChange={(e) => handleValueChange(k, e.target.value)}
            placeholder="Value"
            className="flex-[2] font-mono text-xs"
            type={k.includes("password") || k.includes("secret") || k.includes("key") ? "password" : "text"}
          />
          <Button
            variant="ghost"
            size="icon"
            onClick={() => handleRemove(k)}
            className="text-muted-foreground hover:text-destructive shrink-0"
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      ))}
      <Button
        variant="outline"
        size="sm"
        onClick={handleAdd}
        className="w-full border-dashed"
      >
        <Plus className="mr-2 h-4 w-4" /> 添加配置项
      </Button>
    </div>
  );
}
