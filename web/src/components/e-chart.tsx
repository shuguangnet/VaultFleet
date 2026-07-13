import { useEffect, useRef } from "react";
import { init, use, type EChartsType } from "echarts/core";
import { BarChart, LineChart, PieChart } from "echarts/charts";
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent,
} from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import type { EChartsOption } from "echarts";
import { colors, darkColors } from "@/styles/theme-tokens";
import { useColorMode } from "@/contexts/theme-context";

use([
  BarChart,
  LineChart,
  PieChart,
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent,
  CanvasRenderer,
]);

interface EChartProps {
  option: EChartsOption;
  className?: string;
  height?: number | string;
  loading?: boolean;
}

export function EChart({
  option,
  className,
  height = 280,
  loading = false,
}: EChartProps) {
  const { mode } = useColorMode();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const chartRef = useRef<EChartsType | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;

    const chart = init(containerRef.current, mode === "dark" ? "dark" : undefined, {
      renderer: "canvas",
    });
    chartRef.current = chart;

    const resizeObserver = new ResizeObserver(() => {
      chart.resize();
    });
    resizeObserver.observe(containerRef.current);

    return () => {
      resizeObserver.disconnect();
      chart.dispose();
      chartRef.current = null;
    };
  }, [mode]);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;

    chart.setOption({ ...option, backgroundColor: "transparent" }, true);
  }, [option]);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;

if (loading) {
        chart.showLoading("default", {
          text: "加载中",
          color: mode === "dark" ? darkColors.primary : colors.primary,
          textColor: mode === "dark" ? darkColors.textSecondary : colors.textSecondary,
          maskColor:
            mode === "dark" ? "rgba(24, 33, 43, 0.72)" : "rgba(255, 255, 255, 0.72)",
        });
    } else {
      chart.hideLoading();
    }
  }, [loading, mode]);

  return (
    <div
      ref={containerRef}
      className={className}
      style={{ width: "100%", height }}
    />
  );
}
