// E2E のフィクスチャ。スペックは @playwright/test ではなく、ここから test と expect を取る。
//
//   import { test, expect } from "../support/test.mjs";
//
//   test("行が並ぶ", async ({ app, server }) => {
//     // app は画面を開き終えた Page、server は起動した dwloc edit のハンドル
//   });
//
// 見本と起動の指定は、ファイルか describe ごとに test.use で変える。
//
//   test.use({ repo: sampleRepo({ game: true }) });          // ゲームのフォルダーを使う
//   test.use({ dwloc: { uiLang: "en", locale: "" } });       // 英語の画面、ロケールは画面で選ぶ
//   test.use({ dwloc: { args: ["--verbose"] } });            // 引数を足す
//
// dwloc の指定は既定値（support/dwloc.mjs の DEFAULT_OPTIONS）に重なる。見本は丸ごと
// 差し替わる。1つの試験で2つ目の待ち受けが要るときは launch を呼ぶ。
//
//   const other = await launch({ dwloc: { locale: "he" } });
//
// 用意してあるフィクスチャ:
//   repo             見本（オプション。既定は sampleRepo()）
//   dwloc            起動の指定（オプション。既定は {}）
//   allowPageErrors  頁の中で捕まらなかった例外を許すか（オプション。既定は false）
//   launch           見本を作って dwloc edit を起動する関数。試験の終わりに止めて消す
//   server           既定の見本と指定で起動したハンドル（support/dwloc.mjs の launchDwloc）
//   page             カバレッジを取る Page（Playwright の page を包んだもの）
//   newPage          同じ文脈（Cookie を共有する）でもう1枚の頁を開く関数。カバレッジも取る
//   app              server の URL を開き終えた page
//
// 頁の中で捕まらなかった例外（pageerror）があれば、試験は後始末で落ちる。画面の
// 約束は「黙って失敗しない」なので、例外で処理が途切れたまま通る試験を作らない。
// わざと起こす試験だけ test.use({ allowPageErrors: true }) で外す。
import { test as base, expect } from "@playwright/test";

import { saveCoverage, startCoverage } from "./coverage.mjs";
import { launchDwloc } from "./dwloc.mjs";
import { sampleRepo } from "./repo.mjs";
import { openApp } from "./ui.mjs";

// watchErrors は頁の中で捕まらなかった例外を集める。
function watchErrors(page) {
  const errors = [];
  page.on("pageerror", (err) => errors.push(err));
  return errors;
}

// assertNoErrors は集めた例外があれば投げる。
function assertNoErrors(errors, allowed, label) {
  if (allowed || errors.length === 0) {
    return;
  }
  const lines = errors.map((err) => `  ${err.stack ?? err.message}`);
  throw new Error(`${label}で捕まらなかった例外が ${errors.length} 件ありました:\n${lines.join("\n")}`);
}

export const test = base.extend({
  repo: [sampleRepo(), { option: true }],
  dwloc: [{}, { option: true }],
  allowPageErrors: [false, { option: true }],

  launch: async ({ repo, dwloc }, use) => {
    const handles = [];
    await use(async (override = {}) => {
      const handle = await launchDwloc(override.repo ?? repo, { ...dwloc, ...(override.dwloc ?? {}) });
      handles.push(handle);
      return handle;
    });
    await Promise.all(handles.map((handle) => handle.stop()));
  },

  server: async ({ launch }, use) => {
    await use(await launch());
  },

  page: async ({ page, allowPageErrors }, use, testInfo) => {
    const errors = watchErrors(page);
    await startCoverage(page);
    await use(page);
    // 頁を閉じる前（Playwright の page の後始末より前）に取り出す。
    await saveCoverage(page, testInfo, "page");
    assertNoErrors(errors, allowPageErrors, "頁");
  },

  newPage: async ({ context, allowPageErrors }, use, testInfo) => {
    const opened = [];
    await use(async () => {
      const page = await context.newPage();
      opened.push({ page, errors: watchErrors(page) });
      await startCoverage(page);
      return page;
    });
    for (const [i, { page }] of opened.entries()) {
      await saveCoverage(page, testInfo, `page${i + 2}`);
    }
    for (const [i, { errors }] of opened.entries()) {
      assertNoErrors(errors, allowPageErrors, `${i + 2}枚目の頁`);
    }
  },

  app: async ({ page, server }, use) => {
    await openApp(page, server);
    await use(page);
  },
});

export { expect };
