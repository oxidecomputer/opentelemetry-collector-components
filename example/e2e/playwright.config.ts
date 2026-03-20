import { defineConfig } from "@playwright/test";

const user = process.env.GRAFANA_USER;
const password = process.env.GRAFANA_PASSWORD;

export default defineConfig({
  testDir: ".",
  timeout: 30_000,
  use: {
    baseURL: process.env.GRAFANA_URL || "http://localhost:3000",
    extraHTTPHeaders:
      user && password
        ? {
            Authorization: `Basic ${Buffer.from(`${user}:${password}`).toString("base64")}`,
          }
        : undefined,
  },
});
