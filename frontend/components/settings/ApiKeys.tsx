"use client";

import { useId, useState, type FormEvent } from "react";
import { Badge, Button, Table } from "@/components/ui";
import { GeneratedKey, formatTime } from "@/components/settings/GeneratedKey";
import { QueryState } from "@/components/ui/query-state";
import { ApiError } from "@/lib/api/client";
import type { ApiKey } from "@/lib/api/apikey-schemas";
import {
  useApiKeys,
  useCreateApiKey,
  useRevokeApiKey,
} from "@/lib/hooks/apikeys";

const expiryOptions = [
  { hours: 1, label: "1 hour" },
  { hours: 24, label: "24 hours" },
  { hours: 24 * 7, label: "7 days" },
  { hours: 24 * 30, label: "30 days" },
] as const;

const statusLabel = {
  active: "Active",
  expired: "Expired",
  revoked: "Revoked",
} as const;

function KeyRow({ item }: { item: ApiKey }) {
  const revoke = useRevokeApiKey();
  const [confirming, setConfirming] = useState(false);
  const live = item.status === "active";
  return (
    <tr>
      <td>{item.name}</td>
      <td className="mono">{item.prefix}</td>
      <td>{item.role === "operator" ? "Operator" : "Read only"}</td>
      <td>
        <Badge>{statusLabel[item.status]}</Badge>
      </td>
      <td>{formatTime(item.created_at)}</td>
      <td>{formatTime(item.expires_at)}</td>
      <td>
        {live && !confirming && (
          <Button
            type="button"
            className="button-secondary"
            onClick={() => setConfirming(true)}
            aria-label={`Revoke key ${item.name} (${item.prefix})`}
          >
            Revoke
          </Button>
        )}
        {live && confirming && (
          <span role="group" aria-label={`Confirm revoking ${item.prefix}`}>
            <span className="small">
              Revoking is immediate and cannot be undone.{" "}
            </span>
            <Button
              type="button"
              onClick={() =>
                revoke.mutate(item.id, {
                  onSettled: () => setConfirming(false),
                })
              }
              disabled={revoke.isPending}
            >
              {revoke.isPending ? "Revoking…" : "Confirm revoke"}
            </Button>{" "}
            <Button
              type="button"
              className="button-secondary"
              onClick={() => setConfirming(false)}
              disabled={revoke.isPending}
            >
              Cancel
            </Button>
          </span>
        )}
        {revoke.error && (
          <p className="field-error" role="alert">
            {revoke.error.message}{" "}
            <span className="muted">
              Request ID: <code className="mono">{revoke.error.requestId}</code>
            </span>
          </p>
        )}
      </td>
    </tr>
  );
}

/**
 * Sandbox API key generator (portfolio demo). Operators only. The secret is
 * shown once, in this component's state, and is gone after dismissal or a
 * reload; the API stores only its hash.
 */
export function ApiKeys({ role }: { role: "read" | "operator" }) {
  const id = useId();
  const isOperator = role === "operator";
  const list = useApiKeys(isOperator);
  const create = useCreateApiKey();
  const [name, setName] = useState("Demo key");
  const [keyRole, setKeyRole] = useState<"read" | "operator">("operator");
  const [hours, setHours] = useState<number>(24);
  const created = create.data;

  const submit = (event: FormEvent) => {
    event.preventDefault();
    create.mutate({
      name: name.trim(),
      role: keyRole,
      expires_in_hours: hours,
    });
  };

  const failure = create.error instanceof ApiError ? create.error : null;
  const fieldErrors = failure?.details.field_errors ?? [];

  return (
    <section
      className="card settings-card"
      aria-labelledby="settings-api-keys"
      id="api-keys"
    >
      <h2 id="settings-api-keys">API keys</h2>
      <p>
        Generate a sandbox key to call the API from curl or a script. Keys are
        scoped to this workspace, always expire, and can be revoked. The secret
        is shown once and only its hash is stored. Sandbox only: these keys move
        no real money.
      </p>

      {!isOperator ? (
        <p role="note">
          Generating and listing keys requires the operator role. You are signed
          in as read only.
        </p>
      ) : (
        <>
          <form
            onSubmit={submit}
            className="filter-bar"
            aria-label="Generate API key"
          >
            <div className="field field-wide">
              <label htmlFor={`${id}-name`}>Key name</label>
              <input
                id={`${id}-name`}
                value={name}
                onChange={(event) => setName(event.target.value)}
                maxLength={64}
                required
                autoComplete="off"
                aria-describedby={
                  fieldErrors.some((f) => f.field === "name")
                    ? `${id}-name-error`
                    : undefined
                }
              />
            </div>
            <div className="field">
              <label htmlFor={`${id}-role`}>Role</label>
              <select
                id={`${id}-role`}
                value={keyRole}
                onChange={(event) =>
                  setKeyRole(event.target.value as "read" | "operator")
                }
              >
                <option value="operator">Operator</option>
                <option value="read">Read only</option>
              </select>
            </div>
            <div className="field">
              <label htmlFor={`${id}-expiry`}>Expires after</label>
              <select
                id={`${id}-expiry`}
                value={hours}
                onChange={(event) => setHours(Number(event.target.value))}
              >
                {expiryOptions.map((option) => (
                  <option key={option.hours} value={option.hours}>
                    {option.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <Button type="submit" disabled={create.isPending || !name.trim()}>
                {create.isPending ? "Generating…" : "Generate API key"}
              </Button>
            </div>
          </form>

          <p className="muted small">
            Operator keys can create payments and generate more keys. Read-only
            keys can only read.
          </p>

          {failure && (
            <div className="field-error" role="alert">
              <p>
                {failure.message}{" "}
                <span className="muted">
                  Request ID: <code className="mono">{failure.requestId}</code>
                </span>
              </p>
              {fieldErrors.length > 0 && (
                <ul id={`${id}-name-error`}>
                  {fieldErrors.map((error, index) => (
                    <li key={index}>
                      <span className="mono">{error.field}</span>:{" "}
                      {error.message}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}

          <GeneratedKey created={created} onHide={() => create.reset()} />

          <QueryState
            query={list.query}
            workspace={list.workspace}
            loadingLabel="Loading API keys"
            lines={3}
          >
            {(result) => (
              <>
                <p className="muted small">
                  {
                    result.data.items.filter((k) => k.status === "active")
                      .length
                  }{" "}
                  of {result.data.limit} active keys used.
                </p>
                <Table label="API key list">
                  <thead>
                    <tr>
                      <th scope="col">Name</th>
                      <th scope="col">Prefix</th>
                      <th scope="col">Role</th>
                      <th scope="col">Status</th>
                      <th scope="col">Created</th>
                      <th scope="col">Expires</th>
                      <th scope="col">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {result.data.items.map((item) => (
                      <KeyRow key={item.id} item={item} />
                    ))}
                  </tbody>
                </Table>
              </>
            )}
          </QueryState>
        </>
      )}
    </section>
  );
}
