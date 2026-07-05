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
  const containerRef = useRef<HTMLDivElement | null>(null);
  const chartRef = useRef<EChartsType | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;

    const chart = init(containerRef.current, undefined, {
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
  }, []);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;

    chart.setOption(option, true);
  }, [option]);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;

    if (loading) {
      chart.showLoading("default", {
        text: "加载中",
        color: "#1668dc",
        textColor: "rgba(0, 0, 0, 0.45)",
        maskColor: "rgba(255, 255, 255, 0.6)",
      });
    } else {
      chart.hideLoading();
    }
  }, [loading]);

  return (
    <div
      ref={containerRef}
      className={className}
      style={{ width: "100%", height }}
    />
  );
}
