import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

function RequestChart({ data }) {
  return (
    <div className="chart">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 12, right: 12, bottom: 0, left: 0 }}>
          <CartesianGrid stroke="#282d36" strokeDasharray="4 5" vertical={false} />
          <XAxis
            dataKey="time"
            axisLine={false}
            tickLine={false}
            tick={{ fill: "#818895", fontSize: 11 }}
            minTickGap={24}
          />
          <YAxis
            axisLine={false}
            tickLine={false}
            tick={{ fill: "#818895", fontSize: 11 }}
            width={48}
          />
          <Tooltip
            contentStyle={{
              background: "#191d24",
              border: "1px solid #303641",
              borderRadius: 8,
              color: "#f5f6f8",
            }}
            labelStyle={{ color: "#a6adba" }}
          />
          <Line
            type="monotone"
            dataKey="requests"
            name="Total requests"
            stroke="#7c8cff"
            strokeWidth={2}
            dot={false}
            activeDot={{ r: 4, fill: "#a6b0ff", strokeWidth: 0 }}
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

export default RequestChart;
