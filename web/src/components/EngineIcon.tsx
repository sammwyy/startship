import type { CSSProperties } from "react";
import {
  siPostgresql,
  siMysql,
  siMariadb,
  siMongodb,
  siRedis,
  siRabbitmq,
  siApachekafka,
  siNatsdotio,
  siClickhouse,
  siOpensearch,
  siGitea,
  siJellyfin,
  siTeamspeak,
} from "simple-icons";
import { Icon } from "./Icon";
const brands = {
  gitea: siGitea,
  jellyfin: siJellyfin,
  teamspeak: siTeamspeak,
  postgresql: siPostgresql,
  mysql: siMysql,
  mariadb: siMariadb,
  mongodb: siMongodb,
  redis: siRedis,
  rabbitmq: siRabbitmq,
  kafka: siApachekafka,
  nats: siNatsdotio,
  clickhouse: siClickhouse,
  opensearch: siOpensearch,
};
const glyphs: Record<string, [string, string]> = {
  minecraft: ["box", "#55a047"],
  valkey: ["layers", "#3d9c9b"],
  seaweedfs: ["layers", "#4c82b5"],
};

export function EngineIcon({
  engine,
  fallback = "database",
}: {
  engine: string;
  fallback?: string;
}) {
  const brand = Object.hasOwn(brands, engine)
    ? brands[engine as keyof typeof brands]
    : undefined;
  const glyph = Object.hasOwn(glyphs, engine) ? glyphs[engine] : undefined;
  const dark =
    brand &&
    Math.max(...brand.hex.match(/../g)!.map((v) => parseInt(v, 16))) < 110;
  return (
    <span
      className={`engine-icon ${dark ? "engine-icon-neutral" : ""}`}
      style={
        {
          "--engine-color": brand
            ? `#${brand.hex}`
            : engine === "memcached"
              ? "#549bad"
              : glyph?.[1],
        } as CSSProperties
      }
      aria-hidden="true"
    >
      {brand ? (
        <svg viewBox="0 0 24 24">
          <path d={brand.path} />
        </svg>
      ) : engine === "memcached" ? (
        <span>mc</span>
      ) : (
        <Icon name={glyph?.[0] ?? fallback} />
      )}
    </span>
  );
}
