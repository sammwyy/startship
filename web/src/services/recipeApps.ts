export type Scalar = string | number | boolean;
export type Values = Record<string, Scalar>;
export type Field = {
  type: "string" | "number" | "integer" | "boolean";
  title?: string;
  description?: string;
  default?: Scalar;
  enum?: Scalar[];
  minimum?: number;
  maximum?: number;
  minLength?: number;
  maxLength?: number;
};
export type Schema = {
  properties: Record<string, Field>;
  required?: string[];
};
export type RecipeEndpoint = { id: string; container: string; port: number };
export type RecipePort = RecipeEndpoint & {
  host_port: number;
  protocol: "tcp" | "udp";
};
export type RecipeFile = {
  id: string;
  volume: string;
  path: string;
  format: string;
  schema: Schema;
};
export type Recipe = {
  id: string;
  name: string;
  icon: string;
  description: string;
  version: string;
  config_schema: Schema;
  containers: { id: string; image: string }[];
  volumes: string[] | null;
  http: RecipeEndpoint[] | null;
  ports: RecipePort[] | null;
  files: RecipeFile[] | null;
};
export type AppDomain = {
  id: string;
  host: string;
  endpoint: string;
  certificate_id?: string;
  force_https?: boolean;
};
export type RecipeApp = {
  id: string;
  name: string;
  recipe: Recipe;
  status: string;
  action?: string;
  error?: string;
  running: boolean;
  config: Values | null;
  ports: Record<string, number>;
  domains: AppDomain[];
  containers: Record<string, { status: string; health?: string }>;
};
export type RecipeApps = { apps: RecipeApp[]; can_manage: boolean };

export function filledValues(schema: Schema, values: Values) {
  return Object.fromEntries(
    Object.entries(values).filter(([key, value]) => {
      const type = schema.properties?.[key]?.type;
      return value !== "" || (type !== "integer" && type !== "number");
    }),
  );
}
