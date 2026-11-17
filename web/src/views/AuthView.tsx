import { useNavigate } from "react-router-dom";
import { useSession } from "../providers/SessionProvider";
import { AsyncForm, Field, ThemeButton } from "../components/ui";
import { Brand, Icon } from "../components/Icon";
import { text } from "../lib/project";

export function AuthView() {
  const session = useSession(),
    navigate = useNavigate();
  const force = session.user?.must_change_password,
    setup = session.setupRequired;
  return (
    <div className="auth">
      <header className="auth-header">
        <a className="brand" href="#login">
          <Brand />
        </a>
        <ThemeButton />
      </header>
      <section className="auth-story">
        <div className="auth-index">
          <span>01 — YOUR INFRASTRUCTURE</span>
          <span>BUILT TO SHIP ↗</span>
        </div>
        <div className="auth-message">
          <h1>
            From commit.
            <br />
            To production.
            <br />
            <em>On your terms.</em>
          </h1>
          <p>
            Deploy your applications on the server you own.
            <br />A straight line from your code to the world.
          </p>
        </div>
        <div className="pipeline">
          <div className="pipeline-head">
            <span>THE SHORTEST PATH TO LIVE</span>
            <Icon name="arrow" />
          </div>
          <div className="pipeline-track">
            {[
              ["branch", "Push", "Your repository"],
              ["box", "Build", "Isolated by default"],
              ["globe", "Ship", "Your own domain"],
            ].map(([icon, title, subtitle]) => (
              <div key={title}>
                <span
                  className={`pipeline-node ${title === "Ship" ? "node-live" : ""}`}
                >
                  <Icon name={icon} />
                </span>
                <strong>{title}</strong>
                <span>{subtitle}</span>
              </div>
            ))}
          </div>
          <div className="pipeline-command">
            <span>$</span> git push origin main{" "}
            <span className="command-cursor" />
          </div>
        </div>
      </section>
      <section className="auth-form">
        <AsyncForm
          onSubmit={async (form) => {
            if (force) {
              if (!session.temporaryPassword) {
                await session.logout();
                return;
              }
              const password = text(form, "new_password");
              if (password !== text(form, "confirm"))
                throw new Error("Passwords do not match");
              const bytes = new TextEncoder().encode(password).length;
              if (bytes < 8 || bytes > 72)
                throw new Error("Password must be 8–72 bytes");
              await session.updateAccount({
                current_password: session.temporaryPassword,
                new_password: password,
              });
            } else
              await session.login(
                text(form, "username"),
                text(form, "password"),
                setup,
              );
            navigate("/workspaces");
          }}
        >
          <span className="form-index">
            {force
              ? "COMPLETE SETUP"
              : setup
                ? "FIRST THINGS FIRST"
                : "YOUR CONTROL ROOM"}
          </span>
          <h2>
            {force
              ? "Set your password."
              : setup
                ? "Set up your account."
                : "Welcome back."}
          </h2>
          <p>
            {force
              ? session.temporaryPassword
                ? "Replace the temporary password to continue."
                : "Please sign in again with the temporary password to complete setup."
              : setup
                ? "Create the administrator account for this server."
                : "First run? Find the admin credentials in the server logs (docker compose logs startship)."}
          </p>
          {force ? (
            session.temporaryPassword && (
              <>
                <Field
                  label="New password"
                  name="new_password"
                  type="password"
                  required
                  minLength={8}
                  maxLength={72}
                  autoComplete="new-password"
                />
                <Field
                  label="Confirm password"
                  name="confirm"
                  type="password"
                  required
                  autoComplete="new-password"
                />
              </>
            )
          ) : (
            <>
              <Field
                label="Username"
                name="username"
                required
                autoComplete="username"
                placeholder="Enter your username"
              />
              <Field
                label="Password"
                name="password"
                type="password"
                required
                minLength={8}
                autoComplete={setup ? "new-password" : "current-password"}
                placeholder="Enter your password"
              />
            </>
          )}
          <button className="primary" type="submit">
            {force
              ? session.temporaryPassword
                ? "Set password"
                : "Return to sign in"
              : setup
                ? "Create account"
                : "Sign in"}
            <Icon name="arrow" />
          </button>
          <div className="form-footnote">
            <Icon name="lock" />
            Private access to your infrastructure.
          </div>
        </AsyncForm>
      </section>
      <footer className="auth-footer">
        <span>SELF-HOSTED, NOT LIMITED.</span>
        <span>Startship / Deployment platform</span>
      </footer>
    </div>
  );
}
