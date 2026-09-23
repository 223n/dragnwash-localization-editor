// 画面（internal/web/ui）の E2E の設定。実行は playwright test -c e2e。
//
// 置き場と環境変数の読み方は support/paths.mjs にまとめてある。
//
// 対象は Chromium だけにする。app.js のカバレッジは V8 の precise coverage で取るので、
// Chromium でしか取れない。ヘッドレスで走らせ、retries は 0 にする。揺らぐ試験は
// 再試行で隠さず、揺らがないように直す。
import { defineConfig } from "@playwright/test";

import { outputDir } from "./support/paths.mjs";

const ci = Boolean(process.env.CI);

export default defineConfig({
  testDir: "specs",
  outputDir: outputDir(),
  globalSetup: "./global-setup.mjs",
  // 試験ごとに別の待ち受けと見本を持つので、同じファイルの中でも並べて走らせてよい。
  fullyParallel: true,
  forbidOnly: ci,
  retries: 0,
  workers: ci ? 2 : undefined,
  reporter: ci ? [["github"], ["list"]] : [["list"]],
  use: {
    browserName: "chromium",
    headless: true,
    // 画面の言語は --ui-lang で決めるので、ここは Accept-Language の既定にしかならない。
    locale: "ja-JP",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium" }],
});
