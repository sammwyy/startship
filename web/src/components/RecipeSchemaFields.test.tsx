import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { expect, it } from "vitest";
import type { Values } from "../services/recipeApps";
import { applyLiveMessage } from "../services/live";
import { RecipeSchemaFields } from "./RecipeSchemaFields";

it("renders schema defaults and retains typed values on edits", async () => {
  function Form() {
    const [values, change] = useState<Values>({});
    return (
      <>
        <RecipeSchemaFields
          schema={{
            required: ["players"],
            properties: {
              players: {
                type: "integer",
                title: "Maximum players",
                default: 20,
                minimum: 1,
                maximum: 100,
              },
              online: { type: "boolean", title: "Online", default: true },
              difficulty: {
                type: "string",
                title: "Difficulty",
                enum: ["easy", "hard"],
                default: "easy",
              },
            },
          }}
          values={values}
          change={change}
        />
        <output>{JSON.stringify(values)}</output>
      </>
    );
  }
  const user = userEvent.setup();
  render(<Form />);
  const players = screen.getByRole("spinbutton", { name: "Maximum players" });
  expect(players).toHaveValue(20);
  expect(players).toHaveAttribute("min", "1");
  expect(screen.getByRole("button", { name: "Difficulty" })).toHaveTextContent(
    "easy",
  );
  await user.click(screen.getByRole("checkbox", { name: "Online" }));
  expect(screen.getByText('{"online":false}')).toBeInTheDocument();
  await user.clear(players);
  await user.type(players, "8");
  expect(screen.getByText('{"online":false,"players":8}')).toBeInTheDocument();
});

it("updates the workspace app list from WebSocket snapshots", () => {
  const client = new QueryClient();
  const data = { apps: [{ id: "demo", status: "running" }], can_manage: true };
  applyLiveMessage(
    client,
    { topic: "recipe_apps", workspace_id: "workspace" },
    { topic: "recipe_apps", id: "1", type: "snapshot", data },
  );
  expect(client.getQueryData(["workspaces/workspace/apps"])).toEqual(data);
});
