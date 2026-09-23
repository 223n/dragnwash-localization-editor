// 試験の仕組みのうち、dwloc の起動と後始末（support/dwloc.mjs と global-setup.mjs）を見る。
//
// ここが崩れると、画面の試験は見たいものと関係の無い理由で落ちるか、見本の一時
// ディレクトリ（dwloc-e2e-*）を残していく。どちらも、画面の試験の側からは気付きにくい。
import { copyFile, mkdir, readdir, writeFile } from "node:fs/promises";
import { basename, dirname, isAbsolute, relative } from "node:path";

import { expect, test } from "@playwright/test";

import { binaryPath } from "../support/dwloc.mjs";
import { root } from "../support/paths.mjs";
import { childEnv, describeRun, e2eDir, moduleUrl, runScript, tempEnv } from "./node.mjs";

// leftovers は dir に残った見本の一時ディレクトリを返す。
async function leftovers(dir) {
  return (await readdir(dir)).filter((name) => name.startsWith("dwloc-e2e-"));
}

// privateTemp は子の node に渡す一時ディレクトリの置き場を作る。残ったものを数えるので、
// ほかの試験と同じ置き場を使わない。
async function privateTemp(testInfo) {
  const dir = testInfo.outputPath("tmp");
  await mkdir(dir, { recursive: true });
  return dir;
}

test.describe("DWLOC_BIN", () => {
  test("相対パスはリポジトリのルートから引くので、どこから走らせても、見本の中で起動しても見つかる", async ({}, testInfo) => {
    // README の「ビルド済みの dwloc を使うときは DWLOC_BIN にそのパスを入れます」に従って
    // ./dwloc のように渡す使い方。dwloc は見本の一時ディレクトリをカレントにして起動するので、
    // 相対パスのまま渡すとそこから引かれ、ファイルはあるのに ENOENT で全部の試験が落ちる。
    const bin = testInfo.outputPath("bin", basename(binaryPath()));
    await mkdir(dirname(bin), { recursive: true });
    await copyFile(binaryPath(), bin);
    const rel = relative(root, bin);
    test.skip(isAbsolute(rel), "出力の置き場がリポジトリと別のドライブにあり、相対パスで指せない");
    const tmp = await privateTemp(testInfo);

    // リポジトリのルート（npm run が走らせる場所）と、その下の e2e/ の2か所から走らせる。
    for (const cwd of [root, e2eDir]) {
      const result = await runScript(
        `
        import globalSetup from ${JSON.stringify(moduleUrl("global-setup.mjs"))};
        import { binaryPath, launchDwloc } from ${JSON.stringify(moduleUrl("support", "dwloc.mjs"))};
        import { sampleRepo } from ${JSON.stringify(moduleUrl("support", "repo.mjs"))};
        await globalSetup();
        const server = await launchDwloc(sampleRepo());
        await server.stop();
        console.log(JSON.stringify({ bin: binaryPath(), url: server.url }));
        `,
        {
          cwd,
          env: childEnv({ DWLOC_BIN: rel, DWLOC_E2E_RAW: testInfo.outputPath("raw"), ...tempEnv(tmp) }),
        },
      );
      expect(result.code, describeRun(result)).toBe(0);
      const out = JSON.parse(result.stdout.trim());
      expect(out.bin).toBe(bin);
      expect(out.url).toMatch(/^http:\/\/127\.0\.0\.1:\d+\/\?t=/);
    }
    expect(await leftovers(tmp)).toEqual([]);
  });

  test("起動できないファイルなら、URL を待たずにすぐ失敗し、見本の一時ディレクトリを残さない", async ({}, testInfo) => {
    // 起動の失敗は、Linux では 'exit' ではなく 'error' で届き（EACCES、ENOENT）、Windows では
    // spawn がその場で投げる（EFTYPE）。前者は受け手が無いと捕まらない例外でワーカーごと落ち、
    // URL を待つ側は startTimeout（20秒）まで待つ。どちらでも見本の一時ディレクトリが残る。
    const notBin = testInfo.outputPath("not-dwloc.txt");
    await writeFile(notBin, "dwloc ではない\n");
    const tmp = await privateTemp(testInfo);

    const result = await runScript(
      `
      import { launchDwloc } from ${JSON.stringify(moduleUrl("support", "dwloc.mjs"))};
      import { sampleRepo } from ${JSON.stringify(moduleUrl("support", "repo.mjs"))};
      const started = Date.now();
      try {
        const server = await launchDwloc(sampleRepo());
        await server.stop();
        console.log(JSON.stringify({ launched: true }));
      } catch (err) {
        console.log(JSON.stringify({ launched: false, message: err.message, elapsed: Date.now() - started }));
      }
      `,
      { env: childEnv({ DWLOC_BIN: notBin, ...tempEnv(tmp) }) },
    );
    expect(result.code, describeRun(result)).toBe(0);
    const out = JSON.parse(result.stdout.trim());
    expect(out.launched).toBe(false);
    // 理由には渡したファイルが出る。Windows の「spawn EFTYPE」だけでは、どのファイルを
    // 起動しようとしたのかが分からない。
    expect(out.message).toContain(notBin);
    // startTimeout（20秒）まで待っていないこと。
    expect(out.elapsed).toBeLessThan(10_000);
    expect(await leftovers(tmp)).toEqual([]);
  });
});
