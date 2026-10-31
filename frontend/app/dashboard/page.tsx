import { FeaturePage } from "@/components/feature-page";

export default function OverviewPage() {
  return (
    <FeaturePage
      title="Overview"
      description="An honest view of payment activity, provider performance, and operational attention."
    >
      <div className="metric-grid">
        {["Volume by currency", "Success rate", "Payments", "Observed p95"].map(
          (label) => (
            <section className="metric-card" key={label}>
              <h2>{label}</h2>
              <p className="no-observations">No observations</p>
              <span className="text-label">Payment execution pending</span>
            </section>
          ),
        )}
      </div>
    </FeaturePage>
  );
}
