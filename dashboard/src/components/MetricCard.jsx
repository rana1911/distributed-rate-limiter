function MetricCard({ label, value, detail, tone = "" }) {
  return (
    <article className={`metric-card ${tone}`}>
      <p className="metric-label">{label}</p>
      <p className="metric-value">{value}</p>
      <p className="metric-detail">{detail}</p>
    </article>
  );
}

export default MetricCard;
