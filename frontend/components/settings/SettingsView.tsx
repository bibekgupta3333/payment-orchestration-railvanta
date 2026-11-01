"use client";

import { ThemeSelect } from "@/components/theme";
import { Badge, Table } from "@/components/ui";
import { QueryState } from "@/components/ui/query-state";
import type { Settings } from "@/lib/api/event-schemas";
import { useSettings } from "@/lib/hooks/events";

const providerStatus = {
  enabled: "Enabled",
  disabled: "Disabled",
  not_configured: "Not configured",
} as const;

/** Things this build deliberately does not offer. They are facts, not controls. */
export const unavailableCapabilities = [
  {
    key: "api_key_management",
    label: "API keys",
    note: "Keys are provisioned out of band. They cannot be viewed, created or rotated here.",
  },
  {
    key: "role_management",
    label: "Member roles",
    note: "Roles and membership cannot be changed from this UI.",
  },
  {
    key: "live_activation",
    label: "Live activation",
    note: "Railvanta is sandbox only. Live funds cannot be enabled.",
  },
] as const;

export function SettingsDetails({ settings }: { settings: Settings }) {
  return (
    <>
      <section
        className="card settings-card"
        aria-labelledby="settings-workspace"
      >
        <h2 id="settings-workspace">Workspace</h2>
        <dl className="definition-grid">
          <dt>Name</dt>
          <dd>{settings.workspace.name}</dd>
          <dt>Environment</dt>
          <dd>
            <Badge>Sandbox</Badge> Fixed. This workspace cannot move out of the
            sandbox.
          </dd>
          <dt>Your role</dt>
          <dd>{settings.role === "operator" ? "Operator" : "Read only"}</dd>
        </dl>
      </section>

      <section
        className="card settings-card"
        aria-labelledby="settings-providers"
      >
        <h2 id="settings-providers">Providers</h2>
        <p>
          Read only. Provider configuration is not editable in this milestone.
        </p>
        <Table label="Provider configuration">
          <thead>
            <tr>
              <th scope="col">Provider</th>
              <th scope="col">Status</th>
            </tr>
          </thead>
          <tbody>
            {settings.providers.map((provider) => (
              <tr key={provider.code}>
                <td>{provider.label}</td>
                <td>{providerStatus[provider.status]}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      </section>

      <section
        className="card settings-card"
        aria-labelledby="settings-unavailable"
      >
        <h2 id="settings-unavailable">Unavailable in this build</h2>
        <ul
          className="prose-list"
          style={{ listStyle: "none", paddingLeft: 0 }}
        >
          {unavailableCapabilities.map((item) => (
            <li key={item.key} className="unavailable-row">
              <span>
                <strong>{item.label}</strong>
                <br />
                <span className="text-label">{item.note}</span>
              </span>
              <Badge>Unavailable</Badge>
            </li>
          ))}
        </ul>
      </section>
    </>
  );
}

export function SettingsView() {
  const { query, workspace } = useSettings();
  return (
    <>
      <div className="page-heading">
        <p className="eyebrow">Workspace preferences</p>
        <h1>Settings</h1>
        <p>
          Choose your appearance and review this workspace. Financial and
          account controls are unavailable.
        </p>
      </div>
      <section
        className="card settings-card"
        aria-labelledby="settings-appearance"
      >
        <h2 id="settings-appearance">Appearance</h2>
        <p>Your theme preference stays in this browser.</p>
        <ThemeSelect />
      </section>
      <QueryState
        query={query}
        workspace={workspace}
        loadingLabel="Loading settings"
        lines={4}
      >
        {(result) => <SettingsDetails settings={result.data} />}
      </QueryState>
    </>
  );
}
