import { request } from "./api";

export type FileEntry = {
  name: string;
  type: "directory" | "file" | "symlink" | "hardlink" | "special";
  size: number;
  mode: number;
  mtime: string;
  link?: string;
};

export type FileListing = {
  entries: FileEntry[];
  capabilities: {
    write: boolean;
    move: boolean;
    delete: boolean;
    reason?: string;
  };
};

export const fileEndpoint = (
  target: string,
  operation: string,
  path: string,
  format?: string,
) =>
  `${target}/files/${operation}?${new URLSearchParams({
    path,
    ...(format ? { format } : {}),
  })}`;

export const childPath = (directory: string, name: string) =>
  directory === "/" ? `/${name}` : `${directory}/${name}`;

export const isArchive = (name: string) =>
  /\.(zip|tar|tar\.gz|tgz)$/i.test(name);

export const listFiles = (target: string, path: string, signal: AbortSignal) =>
  request<FileListing>(
    fileEndpoint(target, "list", path),
    undefined,
    "GET",
    signal,
  );
