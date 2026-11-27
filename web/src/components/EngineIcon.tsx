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
  siCockroachlabs,
  siTidb,
  siApachecouchdb,
  siFerretdb,
  siArangodb,
  siNeo4j,
  siSurrealdb,
  siTimescale,
  siInfluxdb,
  siVictoriametrics,
  siApachecassandra,
  siScylladb,
  siApachepulsar,
  siEclipsemosquitto,
  siMinio,
  siMeilisearch,
  siApachesolr,
  siElasticsearch,
  siGrafana,
  siPrometheus,
  siJaeger,
  siOpentelemetry,
  siForgejo,
  siGitlab,
  siJenkins,
  siDrone,
  siHedgedoc,
  siCoder,
  siUptimekuma,
  siMattermost,
  siElement,
  siMumble,
  siJitsi,
  siNextcloud,
  siOwncloud,
  siSeafile,
  siSyncthing,
  siImmich,
  siPaperlessngx,
  siAudiobookshelf,
  siPlex,
  siEmby,
  siCalibreweb,
  siCounterstrike,
  siLuanti,
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
  cockroachdb: siCockroachlabs,
  tidb: siTidb,
  couchdb: siApachecouchdb,
  ferretdb: siFerretdb,
  arangodb: siArangodb,
  neo4j: siNeo4j,
  surrealdb: siSurrealdb,
  timescaledb: siTimescale,
  influxdb: siInfluxdb,
  victoriametrics: siVictoriametrics,
  cassandra: siApachecassandra,
  scylladb: siScylladb,
  pulsar: siApachepulsar,
  mosquitto: siEclipsemosquitto,
  minio: siMinio,
  meilisearch: siMeilisearch,
  solr: siApachesolr,
  elasticsearch: siElasticsearch,
  grafana: siGrafana,
  prometheus: siPrometheus,
  jaeger: siJaeger,
  "opentelemetry-collector": siOpentelemetry,
  forgejo: siForgejo,
  gitlab: siGitlab,
  jenkins: siJenkins,
  "drone-ci": siDrone,
  hedgedoc: siHedgedoc,
  "code-server": siCoder,
  "uptime-kuma": siUptimekuma,
  mattermost: siMattermost,
  element: siElement,
  mumble: siMumble,
  "jitsi-meet": siJitsi,
  nextcloud: siNextcloud,
  owncloud: siOwncloud,
  seafile: siSeafile,
  syncthing: siSyncthing,
  immich: siImmich,
  "paperless-ngx": siPaperlessngx,
  audiobookshelf: siAudiobookshelf,
  plex: siPlex,
  emby: siEmby,
  "calibre-web": siCalibreweb,
  cs2: siCounterstrike,
  luanti: siLuanti,
};
const glyphs: Record<string, [string, string]> = {
  minecraft: ["box", "#55a047"],
  valkey: ["layers", "#3d9c9b"],
  seaweedfs: ["layers", "#4c82b5"],
  yugabytedb: ["database", "#ea7128"],
  percona: ["database", "#d68426"],
  questdb: ["database", "#c569a2"],
  dragonflydb: ["layers", "#21a58b"],
  keydb: ["layers", "#df5b62"],
  foundationdb: ["database", "#7c64bc"],
  firebird: ["database", "#ee671c"],
  activemq: ["pipeline", "#df5d42"],
  artemis: ["pipeline", "#da734f"],
  redpanda: ["pipeline", "#e24d55"],
  emqx: ["pipeline", "#35a2b6"],
  vernemq: ["pipeline", "#288395"],
  typesense: ["search", "#7846ab"],
  loki: ["activity", "#f68c30"],
  vector: ["activity", "#51a691"],
  onedev: ["git-branch", "#4a87ad"],
  "woodpecker-ci": ["pipeline", "#47a566"],
  gogs: ["git-branch", "#6ea456"],
  beszel: ["activity", "#5076d8"],
  "file-browser": ["folder", "#4a8edf"],
  photoprism: ["grid", "#835ad1"],
  sftpgo: ["folder", "#d49d37"],
  navidrome: ["headphones", "#3f9db7"],
  ampache: ["headphones", "#c9633a"],
  metube: ["download", "#d94c4c"],
  komga: ["layers", "#628fce"],
  kavita: ["layers", "#55a075"],
  palworld: ["box", "#5789e4"],
  terraria: ["box", "#5b9d47"],
  valheim: ["box", "#b76c36"],
  "vintage-story": ["box", "#968048"],
  factorio: ["settings", "#cd8838"],
  "project-zomboid": ["box", "#875f58"],
  satisfactory: ["settings", "#d79542"],
  ark: ["box", "#4da5aa"],
  "garrys-mod": ["box", "#43a0d2"],
  "seven-days-to-die": ["box", "#aa5e47"],
  openttd: ["box", "#5b89c0"],
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
