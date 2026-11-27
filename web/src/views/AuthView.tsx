import { useNavigate } from "react-router-dom";
import { useSession } from "../providers/SessionProvider";
import { AsyncForm, Field } from "../components/ui";
import { Icon } from "../components/Icon";
import { InstanceScreen } from "../components/InstanceScreen";
import { text } from "../lib/project";

export function AuthView() {
  const session = useSession(),
    navigate = useNavigate();
  const force = session.user?.must_change_password,
    setup = session.setupRequired;
  return (
    <InstanceScreen>
      <div className="instance-body">
        <AsyncForm
          key={force ? "rotate" : setup ? "setup" : "login"}
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
          <h1>
            {force
              ? "Change temporary password"
              : setup
                ? "Create the administrator"
                : "Sign in to Startship"}
          </h1>
          <p>
            {force
              ? session.temporaryPassword
                ? "Replace the temporary password to continue."
                : "Please sign in again with the temporary password to complete setup."
              : setup
                ? "Create the administrator account for this server."
                : "Use your account on this instance."}
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
            <Icon name="next" />
          </button>
          {!force && !setup && (
            <p className="instance-help">
              First sign-in? Check <code>docker compose logs startship</code>{" "}
              for the initial admin credentials.
            </p>
          )}
        </AsyncForm>
      </div>
    </InstanceScreen>
  );
}
