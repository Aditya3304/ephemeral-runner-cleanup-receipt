import { createRequire as __guardCreateRequire } from 'node:module'; const require = __guardCreateRequire(import.meta.url);

// src/staging.ts
import { readFile } from "node:fs/promises";
import { request } from "node:https";
var MAX_EVIDENCE_BYTES = 900 * 1024;
var MAX_STATUS_BYTES = 16 * 1024;
function checkPayload(data) {
  for (const [key, value] of Object.entries(data)) {
    if (key !== "evidence.json" && key !== "guard.json") throw new Error("Unexpected staging key");
    if (Buffer.byteLength(value) > (key === "evidence.json" ? MAX_EVIDENCE_BYTES : MAX_STATUS_BYTES)) {
      throw new Error(`${key} exceeds staging limit`);
    }
    JSON.parse(value);
  }
}
var stageKubernetes = async (c, data) => {
  checkPayload(data);
  const [ca, tokenBytes] = await Promise.all([readFile(c.ca), readFile(c.token, "utf8")]);
  const token = tokenBytes.trim();
  if (!token || /[\r\n]/.test(token)) throw new Error("Invalid staging token");
  const body = JSON.stringify({ data });
  await new Promise((resolve, reject) => {
    let finished = false;
    const finish = (error) => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      if (error) {
        req.destroy();
        reject(error);
      } else resolve();
    };
    const req = request({
      hostname: c.apiHost.replace(/^\[|\]$/g, ""),
      port: c.apiPort,
      path: `/api/v1/namespaces/${encodeURIComponent(c.stageNamespace)}/configmaps/${encodeURIComponent(c.stageName)}`,
      method: "PATCH",
      ca,
      rejectUnauthorized: true,
      agent: false,
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/merge-patch+json", "Content-Length": Buffer.byteLength(body) }
    }, (res) => {
      if (res.statusCode !== 200) {
        res.destroy();
        finish(new Error(`Staging PATCH rejected: HTTP ${res.statusCode ?? "unknown"}`));
        return;
      }
      let bytes = 0;
      res.on("data", (chunk) => {
        bytes += chunk.length;
        if (bytes > 2 * 1024 * 1024) finish(new Error("Staging response too large"));
      });
      res.on("end", () => finish());
      res.on("error", () => finish(new Error("Staging response failed")));
      res.on("aborted", () => finish(new Error("Staging response aborted")));
    });
    const timer = setTimeout(() => finish(new Error("Staging PATCH timed out")), c.stageTimeoutMs);
    req.on("error", () => finish(new Error("Staging HTTPS connection failed")));
    req.end(body);
  });
};

export {
  MAX_EVIDENCE_BYTES,
  MAX_STATUS_BYTES,
  checkPayload,
  stageKubernetes
};
