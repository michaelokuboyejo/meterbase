"use client"

import { Area, AreaChart, XAxis, YAxis } from "recharts"

import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"
import { cn } from "@/lib/utils"

interface UsageChartProps {
  data: Array<{ bucket: string; value: number }>
  windowSize: string
  label?: string
  className?: string
}

const fmt = new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 })

export function UsageChart({ data, label = "Value", className }: UsageChartProps) {
  const chartConfig = {
    value: { label, color: "var(--chart-1)" },
  } satisfies ChartConfig

  return (
    <ChartContainer config={chartConfig} className={cn("h-40 w-full", className)}>
      <AreaChart data={data} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
        <XAxis
          dataKey="bucket"
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          tickFormatter={(v: string) =>
            new Date(v).toLocaleDateString("en-US", { month: "short", day: "numeric" })
          }
          interval="preserveStartEnd"
        />
        <YAxis
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          width={40}
          tickFormatter={(v: number) => fmt.format(v)}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              labelFormatter={(_, payload) => {
                const bucket = payload?.[0]?.payload?.bucket as string | undefined
                return bucket
                  ? new Date(bucket).toLocaleDateString("en-US", {
                      weekday: "short",
                      month: "short",
                      day: "numeric",
                    })
                  : ""
              }}
            />
          }
        />
        <Area
          dataKey="value"
          stroke="var(--color-value)"
          fill="var(--color-value)"
          fillOpacity={0.15}
          strokeWidth={1.5}
          dot={false}
          activeDot={{ r: 4, strokeWidth: 0 }}
        />
      </AreaChart>
    </ChartContainer>
  )
}
