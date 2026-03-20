import { test, expect } from "@playwright/test";
import * as fs from "fs";
import * as path from "path";

const dashboardDir = path.join(__dirname, "../grafana/dashboards");

// Allow specified panels and sections to be empty, e.g. because
// metrics aren't available or are otherwise expected to be empty.
const allowEmptyPanels = new Set(["5xx error rate"]);
const allowEmptySections = new Set(["sled-agent"]);

const dashboards = fs
  .readdirSync(dashboardDir)
  .filter((f) => f.endsWith(".json"))
  .map((f) => {
    const content = JSON.parse(
      fs.readFileSync(path.join(dashboardDir, f), "utf-8"),
    );
    let section = "";
    const panels = (content.panels || [])
      .map((p: { id: number; title: string; type: string }) => {
        if (p.type === "row") {
          section = p.title;
          return null;
        }
        return { id: p.id, title: p.title, section };
      })
      .filter(Boolean) as { id: number; title: string; section: string }[];
    return {
      file: f,
      uid: content.uid as string,
      title: content.title as string,
      panels,
    };
  });

for (const dashboard of dashboards) {
  test(`${dashboard.title}: all ${dashboard.panels.length} panels render data`, async ({
    page,
  }) => {
    await page.goto(`/d/${dashboard.uid}`);
    await page.waitForLoadState("networkidle");

    // Scroll to the bottom to force Grafana to render all panels.
    for (let attempt = 0; attempt < 20; attempt++) {
      await page.evaluate(() => window.scrollBy(0, 1000));
      await page.waitForTimeout(300);
      const currentCount = await page.locator("[data-viz-panel-key]").count();
      if (currentCount === dashboard.panels.length) {
        break;
      }
    }

    // Wait for panels to finish loading.
    await page.waitForLoadState("networkidle");
    await expect(page.locator('[aria-label="Panel loading bar"]')).toHaveCount(
      0,
      { timeout: 30_000 },
    );

    // Assert the expected number of panels are present.
    const panelLocators = page.locator("[data-viz-panel-key]");
    await expect(panelLocators).toHaveCount(dashboard.panels.length, {
      timeout: 5_000,
    });

    // Assert each panel has rendered content. For panels rendered
    // with <canvas>, the canvas shouldn't be empty. For tables,
    // there should be >0 rows.
    for (let i = 0; i < dashboard.panels.length; i++) {
      const panel = panelLocators.nth(i);
      const key = await panel.getAttribute("data-viz-panel-key");
      const meta = dashboard.panels.find((p) => `panel-${p.id}` === key);

      if (
        meta &&
        (allowEmptyPanels.has(meta.title) ||
          allowEmptySections.has(meta.section))
      ) {
        continue;
      }

      const hasCanvas = (await panel.locator("canvas").count()) > 0;
      if (hasCanvas) {
        const isBlank = await panel
          .locator("canvas")
          .first()
          .evaluate((canvas: HTMLCanvasElement) => {
            const ctx = canvas.getContext("2d");
            if (!ctx) {
              return true;
            }
            const data = ctx.getImageData(
              0,
              0,
              canvas.width,
              canvas.height,
            ).data;
            return data.every((v) => v === 0);
          });
        expect(isBlank, `panel ${key} canvas should not be blank`).toBe(false);
      } else {
        const content = panel.locator(
          ".unwrapped-log-line, .rdg-row:not(.rdg-header-row)",
        );
        await expect(
          content.first(),
          `panel ${key} should have log lines or table rows`,
        ).toBeAttached({ timeout: 10_000 });
      }
    }
  });
}
