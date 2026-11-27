import { text } from "../../lib/project";
import { AsyncForm, ErrorMessage, Field, Loading } from "../../components/ui";
import { useLiveFeed } from "../../providers/LiveProvider";
import { useUI } from "../../providers/UIProvider";
import { request } from "../../services/api";

const events = {
  "uptime.down": "Uptime incident",
  "uptime.recovered": "Uptime recovered",
  "deploy.failed": "Deployment failed",
  "deploy.succeeded": "Deployment succeeded",
};

interface Channel {
  id: string;
  name: string;
  kind: "webhook" | "discord" | "slack";
  host: string;
  events: string[];
  enabled: boolean;
}

interface Delivery {
  id: number;
  channel_id: string;
  event: keyof typeof events | "test";
  status: "ok" | "failed";
  http_status: number;
  error: string;
  created_at: string;
}

interface NotificationSnapshot {
  channels: Channel[];
  deliveries: Delivery[];
}

function ChannelFields({ channel }: { channel?: Channel }) {
  return (
    <>
      <div className="status-settings-fields">
        <Field
          label="Channel name"
          name="name"
          defaultValue={channel?.name}
          maxLength={120}
          required
        />
        <label className="notification-kind">
          Destination type
          <select name="kind" defaultValue={channel?.kind ?? "webhook"}>
            <option value="webhook">Generic HTTP webhook</option>
            <option value="discord">Discord</option>
            <option value="slack">Slack</option>
          </select>
        </label>
      </div>
      <Field
        label={channel ? "Replace webhook URL (optional)" : "Webhook URL"}
        name="url"
        type="password"
        autoComplete="off"
        placeholder={channel?.host ?? "https://"}
        required={!channel}
      />
      <fieldset className="status-picker">
        <legend>Events</legend>
        {Object.entries(events).map(([event, label]) => (
          <label className="status-option" key={event}>
            <input
              type="checkbox"
              name="events"
              value={event}
              defaultChecked={channel?.events.includes(event)}
            />
            {label}
          </label>
        ))}
      </fieldset>
      <label className="status-option">
        <input
          type="checkbox"
          name="enabled"
          defaultChecked={channel?.enabled}
        />
        Enable automatic delivery
      </label>
    </>
  );
}

function channelSettings(form: FormData) {
  const url = text(form, "url");
  return {
    name: text(form, "name"),
    kind: text(form, "kind"),
    ...(url ? { url } : {}),
    events: form.getAll("events"),
    enabled: form.has("enabled"),
  };
}

export function NotificationPanel({
  id,
  data,
}: {
  id: string;
  data: NotificationSnapshot;
}) {
  const ui = useUI();
  const base = `workspaces/${id}/notifications/channels`;
  return (
    <section className="panel panel-pad status-settings notification-settings">
      <div>
        <h2>Notifications</h2>
        <p>Send workspace incident and deployment updates to a webhook.</p>
      </div>
      <AsyncForm
        onSubmit={async (form, element) => {
          await request(base, channelSettings(form), "POST");
          element.reset();
          ui.notify("Notification channel added");
        }}
      >
        <h3>Add a channel</h3>
        <ChannelFields />
        <div className="row">
          <button className="primary">Add channel</button>
        </div>
      </AsyncForm>
      <div>
        <h3>Channels</h3>
        {!data.channels.length && <p>No channels configured.</p>}
        {data.channels.map((channel) => {
          const path = `${base}/${channel.id}`;
          return (
            <details className="notification-channel" key={channel.id}>
              <summary>
                <strong>{channel.name}</strong>
                <span>{channel.host}</span>
                <small>{channel.enabled ? "Enabled" : "Disabled"}</small>
              </summary>
              <AsyncForm
                key={JSON.stringify(channel)}
                onSubmit={async (form, element) => {
                  await request(path, channelSettings(form), "PATCH");
                  const url = element.elements.namedItem("url");
                  if (url instanceof HTMLInputElement) url.value = "";
                  ui.notify("Notification channel saved");
                }}
              >
                <ChannelFields channel={channel} />
                <div className="row notification-actions">
                  <button className="primary">Save channel</button>
                  <button
                    type="button"
                    onClick={() =>
                      ui.run(async () => {
                        await request(`${path}/test`, undefined, "POST");
                        ui.notify(
                          "Test queued. See recent deliveries for the result.",
                        );
                      })
                    }
                  >
                    Send test
                  </button>
                  <button
                    type="button"
                    className="danger"
                    onClick={() =>
                      ui.dialog({
                        title: "Delete notification channel?",
                        description:
                          "This stops delivery to this channel and removes its delivery history.",
                        fields: (
                          <strong className="notification-delete-name">
                            {channel.name}
                          </strong>
                        ),
                        submit: "Delete channel",
                        onSubmit: async () => {
                          await request(path, undefined, "DELETE");
                          ui.notify("Notification channel removed");
                        },
                      })
                    }
                  >
                    Delete channel
                  </button>
                </div>
              </AsyncForm>
            </details>
          );
        })}
      </div>
      <div>
        <h3>Recent deliveries</h3>
        {data.deliveries.length === 0 ? (
          <p>No deliveries yet. Send a test to check your webhook.</p>
        ) : (
          <ul className="notification-deliveries">
            {data.deliveries.map((delivery) => (
              <li key={delivery.id}>
                <strong>
                  {delivery.event === "test"
                    ? "Test notification"
                    : events[delivery.event]}
                </strong>
                <span>
                  {
                    data.channels.find(
                      (channel) => channel.id === delivery.channel_id,
                    )?.name
                  }
                  {" · "}
                  {delivery.status === "ok" ? "Delivered" : "Failed"}
                  {delivery.http_status
                    ? ` · HTTP ${delivery.http_status}`
                    : ""}
                </span>
                <time dateTime={delivery.created_at}>
                  {new Date(delivery.created_at).toLocaleString()}
                </time>
                {delivery.error && <small>{delivery.error}</small>}
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}

export function NotificationSettings({ id }: { id: string }) {
  const feed = useLiveFeed<NotificationSnapshot>({
    topic: "notifications",
    workspace_id: id,
  });
  if (feed.error) return <ErrorMessage error={feed.error} />;
  if (!feed.data) return <Loading />;
  return <NotificationPanel key={id} id={id} data={feed.data} />;
}
