import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import {
  certificateCovers,
  certificateUsable,
  certificateExpiry,
} from "./certificates";
import { applyLiveMessage } from "./live";
import type { Certificate } from "./types";

const certificate: Certificate = {
  id: "c",
  workspace_id: "w",
  domains: ["*.example.com", "example.com"],
  source: "upload",
  issuer: "CA",
  status: "active",
  cert_pem: "PEM",
  not_before: "2020-01-01T00:00:00Z",
  not_after: "2099-01-01T00:00:00Z",
  auto_renew: false,
};
describe("certificate selection and live snapshots", () => {
  it("matches normalized exact names and single-label wildcards only", () => {
    expect(certificateCovers(certificate, "EXAMPLE.COM.")).toBe(true);
    expect(certificateCovers(certificate, "app.example.com")).toBe(true);
    expect(certificateCovers(certificate, "deep.app.example.com")).toBe(false);
    expect(certificateCovers(certificate, "other.test")).toBe(false);
    expect(
      certificateCovers({ ...certificate, domains: [] }, "example.com"),
    ).toBe(false);
  });
  it("keeps valid certificates usable during renewal failures but rejects expired or missing chains", () => {
    expect(certificateUsable({ ...certificate, status: "failed" })).toBe(true);
    expect(certificateUsable({ ...certificate, cert_pem: "" })).toBe(false);
    expect(certificateUsable({ ...certificate, not_after: "2020-02-01" })).toBe(
      false,
    );
    expect(
      certificateExpiry({ ...certificate, not_after: "0001-01-01T00:00:00Z" }),
    ).toBe("Not issued");
  });
  it("replaces workspace certificate lists on issuance, renewal and deletion snapshots", () => {
    const client = new QueryClient();
    const spec = { topic: "certificates", workspace_id: "w" } as const;
    applyLiveMessage(client, spec, {
      id: "1",
      topic: "certificates",
      type: "snapshot",
      data: { certificates: [certificate] },
    });
    expect(client.getQueryData(["workspaces/w/certificates"])).toEqual({
      certificates: [certificate],
    });
    applyLiveMessage(client, spec, {
      id: "1",
      topic: "certificates",
      type: "snapshot",
      data: { certificates: [] },
    });
    expect(client.getQueryData(["workspaces/w/certificates"])).toEqual({
      certificates: [],
    });
    client.clear();
  });
});
