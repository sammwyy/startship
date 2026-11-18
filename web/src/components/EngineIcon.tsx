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
} from "simple-icons";
const brands = {
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
export function EngineIcon({ engine }: { engine: string }) {
  const brand = brands[engine as keyof typeof brands];
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
            : engine === "valkey"
              ? "#3d9c9b"
              : "#8b84ce",
        } as CSSProperties
      }
      aria-hidden="true"
    >
      {brand ? (
        <svg viewBox="0 0 24 24">
          <path d={brand.path} />
        </svg>
      ) : (
        <span>
          {engine === "memcached" ? "mc" : engine === "seaweedfs" ? "sw" : "vk"}
        </span>
      )}
    </span>
  );
}
