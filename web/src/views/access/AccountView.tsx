import type { ReactNode } from "react";
import { Link, Navigate, NavLink, useParams } from "react-router-dom";
import { Heading } from "../../components/Heading";
import { Icon } from "../../components/Icon";
import { AsyncForm, ErrorMessage, Field, Loading } from "../../components/ui";
import { text } from "../../lib/project";
import { setThemePreference, useThemePreference } from "../../lib/theme";
import { useLiveFeed } from "../../providers/LiveProvider";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { remove } from "../../services/api";
import type { AccountSession } from "../../services/types";
import { ActivityFeed, ActivityTime } from "./ActivityView";
import { Tokens } from "./TokensPanel";
import "./AccountView.css";

const tabs = [
  ["", "Profile", "user"],
  ["appearance", "Appearance", "sun"],
  ["sessions", "Sessions", "key"],
  ["tokens", "Tokens", "plug"],
  ["activity", "Activity", "history"],
] as const;

export function AccountView() {
  const { section = "" } = useParams();
  if (!tabs.some(([path]) => path === section))
    return <Navigate to="/account" replace />;
  return (
    <div className="account-page">
      <Heading
        kicker="Personal settings"
        title="Account settings"
        subtitle="Manage your profile, preferences and sign-ins."
      />
      <nav className="account-tabs" aria-label="Account settings sections">
        {tabs.map(([path, label, icon]) => (
          <NavLink key={path} to={path ? `/account/${path}` : "/account"} end>
            <Icon name={icon} />
            {label}
          </NavLink>
        ))}
      </nav>
      {section === "" && <Profile />}
      {section === "appearance" && <Appearance />}
      {section === "sessions" && <Sessions />}
      {section === "tokens" && <Tokens />}
      {section === "activity" && (
        <ActivityFeed spec={{ topic: "account_activity" }} personal />
      )}
    </div>
  );
}

export function PanelHeading({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children?: ReactNode;
}) {
  return (
    <header className="account-panel-header">
      <div>
        <h2>{title}</h2>
        <p className="small">{description}</p>
      </div>
      {children}
    </header>
  );
}

function Profile() {
  const { user, updateAccount } = useSession();
  const { notify } = useUI();
  if (!user) return null;
  return (
    <div className="account-stack">
      <section className="panel account-panel">
        <PanelHeading
          title="Profile details"
          description="Your username is used to sign in. Your email is stored as a contact address."
        />
        <AsyncForm
          key={user.id}
          className="account-form"
          onSubmit={async (data, form) => {
            await updateAccount({
              username: text(data, "username"),
              display_name: text(data, "display_name"),
              email: text(data, "email"),
              current_password: text(data, "current_password"),
            });
            const password = form.elements.namedItem(
              "current_password",
            ) as HTMLInputElement;
            password.value = "";
            notify("Profile saved");
          }}
        >
          <div className="account-fields">
            <Field
              label="Username"
              name="username"
              autoComplete="username"
              required
              maxLength={100}
              defaultValue={user.username}
            />
            <Field
              label="Display name"
              name="display_name"
              autoComplete="name"
              maxLength={100}
              defaultValue={user.display_name}
            />
            <Field
              label="Email address"
              name="email"
              type="email"
              autoComplete="email"
              maxLength={254}
              defaultValue={user.email}
            />
            <Field
              label="Current password"
              name="current_password"
              type="password"
              autoComplete="current-password"
              required
            />
          </div>
          <div className="account-form-actions">
            <small>Changing your username signs out other sessions.</small>
            <button type="submit" className="primary">
              Save profile
            </button>
          </div>
        </AsyncForm>
      </section>
      <section className="panel account-panel">
        <PanelHeading
          title="Password"
          description="Changing your password signs out other sessions. You’ll stay signed in here."
        />
        <AsyncForm
          className="account-form"
          onSubmit={async (data, form) => {
            const password = text(data, "new_password");
            if (password !== text(data, "confirm_password"))
              throw new Error("New passwords do not match.");
            await updateAccount({
              current_password: text(data, "current_password"),
              new_password: password,
            });
            form.reset();
            notify("Password changed");
          }}
        >
          <div className="account-fields account-password">
            <Field
              label="Current password"
              name="current_password"
              type="password"
              autoComplete="current-password"
              required
            />
            <Field
              label="New password"
              name="new_password"
              type="password"
              autoComplete="new-password"
              minLength={8}
              maxLength={72}
              required
            />
            <Field
              label="Confirm new password"
              name="confirm_password"
              type="password"
              autoComplete="new-password"
              required
            />
          </div>
          <div className="account-form-actions">
            <small>Use at least 8 characters.</small>
            <button type="submit" className="primary">
              Change password
            </button>
          </div>
        </AsyncForm>
      </section>
    </div>
  );
}

const themes = [
  ["system", "System", "Follow your device", "server"],
  ["light", "Light", "Always use light colors", "sun"],
  ["dark", "Dark", "Always use dark colors", "moon"],
] as const;

function Appearance() {
  const selected = useThemePreference();
  return (
    <section className="panel account-panel">
      <PanelHeading
        title="Color theme"
        description="Choose how Startship looks in this browser."
      />
      <fieldset className="account-theme-options" aria-label="Color theme">
        {themes.map(([value, name, description, icon]) => (
          <label key={value} className="account-theme">
            <input
              type="radio"
              name="theme"
              value={value}
              checked={selected === value}
              onChange={() => setThemePreference(value)}
            />
            <Icon name={icon} />
            <span>
              <strong>{name}</strong>
              <small>{description}</small>
            </span>
            <span className="account-theme-indicator" aria-hidden="true" />
          </label>
        ))}
      </fieldset>
    </section>
  );
}

function clientName(agent: string) {
  if (/startship-cli/i.test(agent)) return "Startship CLI";
  if (/Edg\//.test(agent)) return "Microsoft Edge";
  if (/Firefox|FxiOS/.test(agent)) return "Firefox";
  if (/Chrome|CriOS/.test(agent)) return "Chrome";
  if (/Safari/.test(agent)) return "Safari";
  return "Other client";
}

function Sessions() {
  const { user, logout } = useSession();
  const { dialog, notify } = useUI();
  const feed = useLiveFeed<AccountSession[]>({ topic: "account_sessions" });
  const sessions = [...(feed.data ?? [])].sort(
    (a, b) =>
      Number(b.id === user?.session_id) - Number(a.id === user?.session_id),
  );
  function revoke(session: AccountSession) {
    const current = session.id === user?.session_id;
    dialog({
      title: current ? "Sign out of this session?" : "Revoke this session?",
      description: current
        ? "You’ll need to sign in again on this device."
        : "This device will lose access and must sign in again.",
      submit: current ? "Sign out" : "Revoke session",
      onSubmit: async () => {
        if (current) await logout();
        else {
          await remove(`me/sessions/${encodeURIComponent(session.id)}`);
          notify("Session revoked");
        }
      },
    });
  }
  return (
    <section className="panel account-panel">
      <PanelHeading
        title="Active sessions"
        description="Review where you’re signed in and end sessions you don’t recognize."
      />
      {feed.error && <ErrorMessage error={feed.error} />}
      {!feed.error && !feed.data && <Loading />}
      {feed.data && sessions.length === 0 && (
        <p className="activity-empty small">No active sessions.</p>
      )}
      <ul className="account-sessions">
        {sessions.map((session) => {
          const current = session.id === user?.session_id;
          return (
            <li key={session.id}>
              <div className="session-summary">
                <span className="activity-icon">
                  <Icon name="terminal" />
                </span>
                <div className="session-client">
                  <div className="session-title">
                    <strong>{clientName(session.user_agent)}</strong>
                    {current && (
                      <span className="session-current">This session</span>
                    )}
                  </div>
                  <p className="small session-agent">
                    {session.user_agent || "User agent unavailable"}
                  </p>
                </div>
                <button onClick={() => revoke(session)}>
                  {current ? "Sign out" : "Revoke"}
                </button>
              </div>
              <dl className="session-facts">
                <div>
                  <dt>IP address</dt>
                  <dd>{session.ip || "Unavailable"}</dd>
                </div>
                <div>
                  <dt>Signed in</dt>
                  <dd>
                    <ActivityTime value={session.logged_at} />
                  </dd>
                </div>
                <div>
                  <dt>Last active</dt>
                  <dd>
                    <ActivityTime value={session.last_activity_at} />
                  </dd>
                </div>
                <div>
                  <dt>Session</dt>
                  <dd>
                    <Link
                      to={`/account/activity?session=${encodeURIComponent(session.id)}`}
                      title={session.id}
                    >
                      {session.id.slice(0, 8)} <span aria-hidden="true">↗</span>
                    </Link>
                  </dd>
                </div>
              </dl>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
