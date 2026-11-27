import type { ReactNode } from "react";
import { Brand, Icon } from "./Icon";
import { ThemeButton } from "./ui";
import "./InstanceScreen.css";

export function InstanceScreen({ children }: { children: ReactNode }) {
  return (
    <div className="instance-screen">
      <header className="instance-header">
        <span className="brand">
          <Brand />
        </span>
        <ThemeButton />
      </header>
      <main className="instance-main">
        <div className="instance-card">
          <div className="instance-label-row">
            <Icon name="server" />
            Self-hosted instance
          </div>
          {children}
          <footer className="instance-host">
            <span>Instance host</span>
            <code>{window.location.host}</code>
          </footer>
        </div>
      </main>
    </div>
  );
}

export function BootScreen() {
  return (
    <InstanceScreen>
      <div
        className="instance-body"
        role="status"
        aria-label="Loading Startship"
      >
        <h1>Loading Startship</h1>
        <p>Checking access to this instance.</p>
        <div className="instance-progress" aria-hidden="true" />
      </div>
    </InstanceScreen>
  );
}
