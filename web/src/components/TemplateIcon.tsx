import type { CSSProperties } from "react";
import {
  siNodedotjs,
  siBun,
  siNextdotjs,
  siVite,
  siAstro,
  siNuxt,
  siSvelte,
  siPython,
  siDjango,
  siFastapi,
  siFlask,
  siGo,
  siRust,
  siDocker,
  siHtml5,
} from "simple-icons";
import { Icon } from "./Icon";

const brands = {
  nodedotjs: siNodedotjs,
  bun: siBun,
  nextdotjs: siNextdotjs,
  vite: siVite,
  astro: siAstro,
  nuxt: siNuxt,
  svelte: siSvelte,
  python: siPython,
  django: siDjango,
  fastapi: siFastapi,
  flask: siFlask,
  go: siGo,
  rust: siRust,
  docker: siDocker,
  html5: siHtml5,
};
export function TemplateIcon({ slug }: { slug: string }) {
  const brand = brands[slug as keyof typeof brands];
  const dark =
    brand &&
    Math.max(...brand.hex.match(/../g)!.map((v) => parseInt(v, 16))) < 110;
  return (
    <span
      className={`template-icon${dark ? " template-icon-neutral" : ""}`}
      style={
        brand ? ({ "--brand": `#${brand.hex}` } as CSSProperties) : undefined
      }
    >
      {brand ? (
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d={brand.path} />
        </svg>
      ) : (
        <Icon name={slug === "auto" ? "bolt" : "wrench"} />
      )}
    </span>
  );
}
