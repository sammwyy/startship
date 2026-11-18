import { useState } from "react";
import { Link } from "react-router-dom";
import { Icon } from "../../components/Icon";
import { CopyValue } from "../../components/ServiceUI";
import { AsyncForm, ErrorMessage, Field, Loading } from "../../components/ui";
import { useDashboard } from "../../layouts/DashboardLayout";
import { useLiveFeed } from "../../providers/LiveProvider";
import { useUI } from "../../providers/UIProvider";
import { remove, request } from "../../services/api";
import type { AccessToken } from "../../services/types";
import { PanelHeading } from "./AccountView";
import { ActivityTime } from "./ActivityView";
import "../../components/services.css";
import "./TokensPanel.css";

type Created = AccessToken & { token: string };

const accessLevels = [
  [
    "operate",
    "Operate",
    "View, create, configure and deploy projects.",
    "rocket",
  ],
  [
    "admin",
    "Administer",
    "Also manage members, settings, services and deletions.",
    "crown",
  ],
] as const;
const expiries = [
  ["30", "30 days"],
  ["90", "90 days"],
  ["365", "1 year"],
  ["0", "Never"],
] as const;

export function mcpEndpoint() {
  return `${window.location.origin}/api/mcp`;
}

export function mcpConfig(token: string) {
  return JSON.stringify(
    {
      mcpServers: {
        startship: {
          type: "http",
          url: mcpEndpoint(),
          headers: { Authorization: `Bearer ${token}` },
        },
      },
    },
    null,
    2,
  );
}

export function Tokens() {
  const feed = useLiveFeed<AccessToken[]>({ topic: "account_tokens" });
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<Created>();
  return (
    <div className="account-stack">
      <section className="panel account-panel">
        <PanelHeading
          title="MCP server"
          description="Connect AI assistants to Startship. They act as you, limited to what each token allows."
        />
        <div className="token-endpoint">
          <span className="activity-icon">
            <Icon name="plug" />
          </span>
          <div>
            <strong>Streamable HTTP endpoint</strong>
            <CopyValue value={mcpEndpoint()} />
          </div>
          <p className="small">
            Send a token with MCP enabled as{" "}
            <code>Authorization: Bearer …</code>
          </p>
        </div>
      </section>
      {created && (
        <CreatedToken token={created} onDone={() => setCreated(undefined)} />
      )}
      <section className="panel account-panel">
        <PanelHeading
          title="Access tokens"
          description="Tokens sign in the API, the CLI and MCP clients without your password. Revoke any you no longer use."
        >
          {!creating && (
            <button
              className="primary"
              onClick={() => {
                setCreated(undefined);
                setCreating(true);
              }}
            >
              <Icon name="plus" />
              New token
            </button>
          )}
        </PanelHeading>
        {creating && (
          <NewToken
            onCancel={() => setCreating(false)}
            onCreated={(token) => {
              setCreating(false);
              setCreated(token);
            }}
          />
        )}
        {feed.error && <ErrorMessage error={feed.error} />}
        {!feed.error && !feed.data && <Loading />}
        {feed.data?.length === 0 && !creating && (
          <div className="activity-empty small">
            <Icon name="key" />
            <p>No access tokens yet.</p>
          </div>
        )}
        <ul className="account-sessions">
          {feed.data?.map((token) => (
            <TokenRow key={token.id} token={token} />
          ))}
        </ul>
      </section>
    </div>
  );
}

function CopyButton({ value }: { value: string }) {
  const ui = useUI();
  return (
    <button
      type="button"
      className="icon-button"
      aria-label="Copy configuration"
      title="Copy configuration"
      onClick={() =>
        ui.run(async () => {
          await navigator.clipboard.writeText(value);
          ui.notify("Copied to clipboard");
        })
      }
    >
      <Icon name="copy" />
    </button>
  );
}

function CreatedToken({
  token,
  onDone,
}: {
  token: Created;
  onDone: () => void;
}) {
  return (
    <section className="panel account-panel token-created" aria-live="polite">
      <PanelHeading
        title={`Token “${token.name}” created`}
        description="Copy it now. Startship stores only a hash, so it can’t be shown again."
      >
        <button onClick={onDone}>Done</button>
      </PanelHeading>
      <div className="token-secret">
        <CopyValue value={token.token} />
        {token.mcp && (
          <>
            <p className="small">
              MCP client configuration (Claude Code, Cursor, VS Code and others
              that support Streamable HTTP):
            </p>
            <div className="token-config">
              <pre>{mcpConfig(token.token)}</pre>
              <CopyButton value={mcpConfig(token.token)} />
            </div>
          </>
        )}
        {token.api && (
          <p className="small">
            CLI:{" "}
            <code>STARTSHIP_TOKEN={token.prefix}… startship workspaces</code>
          </p>
        )}
      </div>
    </section>
  );
}

function NewToken({
  onCancel,
  onCreated,
}: {
  onCancel: () => void;
  onCreated: (token: Created) => void;
}) {
  const { workspaces } = useDashboard();
  const { notify } = useUI();
  const [all, setAll] = useState(workspaces.length !== 1);
  const [selected, setSelected] = useState<string[]>(
    workspaces.length === 1 ? [workspaces[0].id] : [],
  );
  const [api, setAPI] = useState(true);
  const [mcp, setMCP] = useState(true);
  return (
    <AsyncForm
      className="token-form"
      disabled={(!all && selected.length === 0) || (!api && !mcp)}
      onSubmit={async (data) => {
        const token = await request<Created>("me/tokens", {
          name: String(data.get("name") || ""),
          access: data.get("access"),
          all_workspaces: all,
          workspaces: all ? [] : selected,
          api,
          mcp,
          expires_in_days: Number(data.get("expiry")),
        });
        notify("Token created");
        onCreated(token);
      }}
    >
      <div className="token-form-grid">
        <Field
          label="Name"
          name="name"
          required
          maxLength={60}
          placeholder="Claude on my laptop"
          autoFocus
        />
        <fieldset className="token-choice">
          <legend>Expires</legend>
          <div className="token-segments">
            {expiries.map(([value, label]) => (
              <label key={value}>
                <input
                  type="radio"
                  name="expiry"
                  value={value}
                  defaultChecked={value === "90"}
                />
                <span>{label}</span>
              </label>
            ))}
          </div>
        </fieldset>
      </div>
      <fieldset className="token-choice">
        <legend>Access</legend>
        <div className="token-cards">
          {accessLevels.map(([value, label, description, icon]) => (
            <label key={value} className="account-theme">
              <input
                type="radio"
                name="access"
                value={value}
                defaultChecked={value === "operate"}
              />
              <Icon name={icon} />
              <span>
                <strong>{label}</strong>
                <small>{description}</small>
              </span>
              <span className="account-theme-indicator" aria-hidden="true" />
            </label>
          ))}
        </div>
      </fieldset>
      <fieldset className="token-choice">
        <legend>Workspaces</legend>
        <div className="token-cards">
          <label className="account-theme">
            <input
              type="radio"
              name="scope"
              checked={all}
              onChange={() => setAll(true)}
            />
            <Icon name="globe" />
            <span>
              <strong>All workspaces</strong>
              <small>Including workspaces created later.</small>
            </span>
            <span className="account-theme-indicator" aria-hidden="true" />
          </label>
          <label className="account-theme">
            <input
              type="radio"
              name="scope"
              checked={!all}
              onChange={() => setAll(false)}
            />
            <Icon name="grid" />
            <span>
              <strong>Selected workspaces</strong>
              <small>Only the ones you pick below.</small>
            </span>
            <span className="account-theme-indicator" aria-hidden="true" />
          </label>
        </div>
        {!all && (
          <div className="token-workspaces">
            {workspaces.map((w) => (
              <label key={w.id} className="token-check">
                <input
                  type="checkbox"
                  checked={selected.includes(w.id)}
                  onChange={(e) =>
                    setSelected(
                      e.target.checked
                        ? [...selected, w.id]
                        : selected.filter((id) => id !== w.id),
                    )
                  }
                />
                <span className="token-check-box" aria-hidden="true">
                  <Icon name="check" />
                </span>
                {w.name}
              </label>
            ))}
            {workspaces.length === 0 && (
              <p className="small">You aren’t a member of any workspace.</p>
            )}
          </div>
        )}
      </fieldset>
      <fieldset className="token-choice">
        <legend>Use with</legend>
        <div className="token-workspaces">
          <label className="token-check">
            <input
              type="checkbox"
              checked={mcp}
              onChange={(e) => setMCP(e.target.checked)}
            />
            <span className="token-check-box" aria-hidden="true">
              <Icon name="check" />
            </span>
            MCP clients
          </label>
          <label className="token-check">
            <input
              type="checkbox"
              checked={api}
              onChange={(e) => setAPI(e.target.checked)}
            />
            <span className="token-check-box" aria-hidden="true">
              <Icon name="check" />
            </span>
            API and CLI
          </label>
        </div>
      </fieldset>
      <p className="small token-note">
        A token never exceeds your own permissions, can’t manage your account
        and stops working if your access is removed.
      </p>
      <div className="account-form-actions">
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
        <button type="submit" className="primary">
          Create token
        </button>
      </div>
    </AsyncForm>
  );
}

function TokenRow({ token }: { token: AccessToken }) {
  const { workspaces } = useDashboard();
  const { dialog, notify } = useUI();
  const names = token.workspaces.map(
    (id) => workspaces.find((w) => w.id === id)?.name ?? "Removed workspace",
  );
  const scope = token.all_workspaces
    ? "All workspaces"
    : names.length > 2
      ? `${names.slice(0, 2).join(", ")} +${names.length - 2}`
      : names.join(", ");
  function revoke() {
    dialog({
      title: `Revoke “${token.name}”?`,
      description:
        "Anything using this token loses access immediately. This can’t be undone.",
      submit: "Revoke token",
      onSubmit: async () => {
        await remove(`me/tokens/${encodeURIComponent(token.id)}`);
        notify("Token revoked");
      },
    });
  }
  return (
    <li>
      <div className="session-summary">
        <span className="activity-icon">
          <Icon name={token.access === "admin" ? "crown" : "key"} />
        </span>
        <div className="session-client">
          <div className="session-title">
            <strong>{token.name}</strong>
            <span className="session-current token-access">
              {token.access === "admin" ? "Administer" : "Operate"}
            </span>
            {token.mcp && <span className="activity-scope">MCP</span>}
            {token.api && <span className="activity-scope">API</span>}
          </div>
          <p className="small session-agent" title={names.join(", ")}>
            <code>{token.prefix}…</code> · {scope}
          </p>
        </div>
        <button onClick={revoke}>Revoke</button>
      </div>
      <dl className="session-facts">
        <div>
          <dt>Last used from</dt>
          <dd>{token.last_used_ip || "Never used"}</dd>
        </div>
        <div>
          <dt>Last used</dt>
          <dd>
            {token.last_used_at ? (
              <ActivityTime value={token.last_used_at} />
            ) : (
              "Never"
            )}
          </dd>
        </div>
        <div>
          <dt>Created</dt>
          <dd>
            <ActivityTime value={token.created_at} />
          </dd>
        </div>
        <div>
          <dt>Expires</dt>
          <dd>
            {token.expires_at ? (
              <ActivityTime value={token.expires_at} />
            ) : (
              "Never"
            )}
          </dd>
        </div>
      </dl>
      <p className="small token-activity">
        <Link to="/account/activity">Activity</Link>
        {token.last_user_agent && <> · {token.last_user_agent}</>}
      </p>
    </li>
  );
}
