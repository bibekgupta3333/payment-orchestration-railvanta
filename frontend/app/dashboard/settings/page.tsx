import { ThemeSelect } from "@/components/theme";
import { UnavailableState } from "@/components/ui";

export default function SettingsPage() {
  return <><div className="page-heading"><p className="eyebrow">Workspace preferences</p><h1>Settings</h1><p>Choose your appearance. Financial and account controls remain unavailable.</p></div><section className="card settings-card"><h2>Appearance</h2><p>Your theme preference stays in this browser.</p><ThemeSelect /></section><UnavailableState title="Workspace configuration unavailable">The environment is fixed to sandbox. Provider configuration, key management, membership changes, and live activation are not available in this UI.</UnavailableState></>;
}
