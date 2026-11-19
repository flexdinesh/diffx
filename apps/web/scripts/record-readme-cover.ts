// Films the README cover (packages/review-app ReadmeCover) frame by frame on
// stageshow's virtual clock and encodes each theme to an animated WebP in
// docs-website/public. Run with `mise run readme:cover`. Frames are 2x for
// retina and lossless: lossy WebP halves chroma (colored code text smears)
// and its near-match frame diffs leave ghosts of earlier scenes.

import { chromium } from "@playwright/test";
import { installVirtualClock } from "@diffx/stageshow/clock";
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const FPS = 60;
const SCALE = 2;
const MAX_FRAMES = FPS * 40;
const webRoot = fileURLToPath(new URL("..", import.meta.url));
const outDir = fileURLToPath(
  new URL("../../../docs-website/public/", import.meta.url),
);

// Any API URL keeps vite.config.ts from starting its Go fixture server; the
// capture page never calls the API.
process.env.DIFFX_API_URL ??= "http://127.0.0.1:9";
const server = await createServer({
  root: webRoot,
  configFile: join(webRoot, "vite.config.ts"),
  logLevel: "warn",
  // No reloads mid-take: an edited file must not restart the scene.
  server: { host: "127.0.0.1", port: 0, hmr: false, watch: null },
});
await server.listen();
const address = server.resolvedUrls?.local[0];
if (!address) throw new Error("Vite did not report a local URL");

const browser = await chromium.launch();
try {
  const only = process.argv[2];
  const themes = (["light", "dark"] as const).filter(
    (theme) => only === undefined || theme === only,
  );
  for (const theme of themes) {
    const frames = mkdtempSync(join(tmpdir(), `diffx-cover-${theme}-`));
    try {
      const count = await film(theme, frames);
      const out = join(outDir, `readme-cover-${theme}.webp`);
      encode(frames, out);
      const kb = Math.round(statSync(out).size / 1024);
      console.log(`${out}: ${count} frames, ${kb} KB`);
    } finally {
      rmSync(frames, { recursive: true, force: true });
    }
  }
} finally {
  await browser.close();
  await server.close();
}

async function film(theme: "light" | "dark", dir: string): Promise<number> {
  const page = await browser.newPage({
    viewport: { width: 1000, height: 700 },
    deviceScaleFactor: SCALE,
    colorScheme: theme,
    reducedMotion: "no-preference",
  });
  const clockSource = installVirtualClock.toString();
  await page.addInitScript(
    `window.__stageshow = { theme: ${JSON.stringify(theme)} };` +
      `(${clockSource})({ fps: ${FPS}, epoch: 0 });`,
  );
  await page.goto(`${address}capture/readme-cover.html?theme=${theme}`);
  const stage = page.locator("#cover .ss-stage").first();
  await stage.waitFor();
  await page.evaluate(() => document.fonts.ready);
  let frame = 0;
  for (; frame < MAX_FRAMES; frame++) {
    await stage.screenshot({
      path: join(dir, `${String(frame).padStart(4, "0")}.png`),
    });
    if ((await stage.getAttribute("data-mark")) === "end") break;
    await page.evaluate(async () => {
      const clock = window.__stageshow?.clock;
      if (!clock) throw new Error("Virtual clock missing");
      await clock.step();
    });
  }
  await page.close();
  if (frame === MAX_FRAMES) throw new Error("Cover never reached its end mark");
  return frame + 1;
}

function encode(dir: string, out: string) {
  execFileSync(
    "ffmpeg",
    [
      "-hide_banner",
      "-loglevel",
      "error",
      "-y",
      "-framerate",
      String(FPS),
      "-i",
      join(dir, "%04d.png"),
      "-c:v",
      "libwebp_anim",
      "-pix_fmt",
      "bgra",
      "-lossless",
      "1",
      "-quality",
      "100",
      "-compression_level",
      "6",
      "-loop",
      "0",
      out,
    ],
    { stdio: "inherit" },
  );
}
