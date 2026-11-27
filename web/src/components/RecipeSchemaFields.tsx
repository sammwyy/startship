import type { Schema, Values } from "../services/recipeApps";
import { ServicePicker } from "./ServiceUI";

export function RecipeSchemaFields({
  schema,
  values,
  change,
  disabled,
}: {
  schema: Schema;
  values: Values;
  change: (values: Values) => void;
  disabled?: boolean;
}) {
  return Object.entries(schema.properties || {}).map(([key, field]) => {
    const title = field.title || key;
    const value = values[key] ?? field.default;
    const required = schema.required?.includes(key);
    return (
      <div className="recipe-field" key={key}>
        {field.enum ? (
          <ServicePicker
            label={title}
            value={value === undefined ? "" : JSON.stringify(value)}
            options={field.enum.map((option) => ({
              value: JSON.stringify(option),
              label: String(option),
            }))}
            onChange={(option) =>
              change({ ...values, [key]: JSON.parse(option) })
            }
            disabled={disabled}
          />
        ) : (
          <label className={field.type === "boolean" ? "recipe-check" : ""}>
            <span>{title}</span>
            {field.type === "boolean" ? (
              <input
                type="checkbox"
                checked={value === true}
                disabled={disabled}
                onChange={(event) =>
                  change({ ...values, [key]: event.target.checked })
                }
              />
            ) : (
              <input
                type={field.type === "string" ? "text" : "number"}
                required={required}
                min={field.minimum}
                max={field.maximum}
                minLength={field.minLength}
                maxLength={field.maxLength}
                step={field.type === "integer" ? "1" : "any"}
                value={typeof value === "boolean" ? "" : (value ?? "")}
                disabled={disabled}
                onChange={(event) => {
                  const input = event.currentTarget;
                  const next = { ...values };
                  if (field.type === "string") next[key] = input.value;
                  else if (input.value === "") next[key] = "";
                  else next[key] = input.valueAsNumber;
                  change(next);
                }}
              />
            )}
          </label>
        )}
        {field.description && (
          <p className="small muted">{field.description}</p>
        )}
      </div>
    );
  });
}
