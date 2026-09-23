// 試験の仕組みのうち、カバレッジの集計（coverage.mjs）を見る。
//
// 集計は子の node で `node e2e/coverage.mjs <命令>` として走らせ、置き場は DWLOC_E2E_RAW と
// DWLOC_E2E_REPORT で試験ごとの一時ディレクトリへ向ける。既定の置き場（coverage/）は、
// いま走っている実行が生データを書いている場所なので触らない。
import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { mkdir, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";

import { expect, test } from "@playwright/test";

import { appJs } from "../support/paths.mjs";
import { childEnv, describeRun, e2eDir, runNode } from "./node.mjs";

const source = readFileSync(appJs, "utf8");
const sourceHash = createHash("sha256").update(source, "utf8").digest("hex");

// rawBody は生データ1ファイルの中身を作る（support/coverage.mjs の saveCoverage と同じ形）。
// app.js の全体を1回通ったことにする。ここでは数字は見ない。
function rawBody(run, { hash = sourceHash } = {}) {
  return JSON.stringify({
    run,
    test: "試験の仕組み > 見本の生データ",
    file: "harness",
    entries: [
      {
        url: "/app.js",
        sourceHash: hash,
        functions: [
          {
            functionName: "",
            isBlockCoverage: true,
            ranges: [{ startOffset: 0, endOffset: source.length, count: 1 }],
          },
        ],
      },
    ],
  });
}

// writeTree は base の下にファイルを置く。
async function writeTree(base, files) {
  await mkdir(base, { recursive: true });
  for (const [rel, body] of Object.entries(files)) {
    const path = join(base, rel);
    await mkdir(dirname(path), { recursive: true });
    await writeFile(path, body);
  }
}

// coverage は集計を子の node で走らせる。
function coverage(command, { raw, report }) {
  return runNode([join(e2eDir, "coverage.mjs"), command], {
    env: childEnv({ DWLOC_E2E_RAW: raw, DWLOC_E2E_REPORT: report }),
  });
}

test.describe("前の実行の生データ", () => {
  test("2回分の実行の生データが混ざっていたら、数えずに止めて clean を案内する", async ({}, testInfo) => {
    // 前の実行の生データが残っていると、スペックを絞った実行の数字が水増しされて閾値を
    // 通ってしまう。app.js を直したあとなら、古い生データのせいで「ビルドし直せ」と止まり、
    // ビルドし直しても直らない。
    const raw = testInfo.outputPath("raw");
    const report = testInfo.outputPath("report");
    await writeTree(raw, {
      "old.json": rawBody("1000-aaaaaaaa"),
      "new.json": rawBody("2000-bbbbbbbb"),
    });

    const result = await coverage("report", { raw, report });
    expect(result.code, describeRun(result)).toBe(1);
    expect(result.stderr).toContain("1000-aaaaaaaa");
    expect(result.stderr).toContain("2000-bbbbbbbb");
    expect(result.stderr).toContain("node e2e/coverage.mjs clean");
    // 混ざった数字の報告は書かない。
    expect(existsSync(report)).toBe(false);
  });

  test("実行の ID が無い（前の形の）生データも、数えずに止めて clean を案内する", async ({}, testInfo) => {
    // どの実行のものかが分からないので、今回の実行のものとして数えられない。
    const raw = testInfo.outputPath("raw");
    const report = testInfo.outputPath("report");
    await writeTree(raw, { "legacy.json": rawBody(undefined) });

    const result = await coverage("report", { raw, report });
    expect(result.code, describeRun(result)).toBe(1);
    expect(result.stderr).toContain("legacy.json");
    expect(result.stderr).toContain("node e2e/coverage.mjs clean");
    expect(existsSync(report)).toBe(false);
  });

  test("配られた app.js と食い違う生データなら、ビルドし直すことと clean の両方を案内する", async ({}, testInfo) => {
    // 食い違いの原因は、古いバイナリ（DWLOC_BIN）だけではない。試験のあとで app.js を
    // 直したときも起き、そのときはビルドし直しても直らない。
    const raw = testInfo.outputPath("raw");
    const report = testInfo.outputPath("report");
    await writeTree(raw, { "stale.json": rawBody("1000-aaaaaaaa", { hash: "0".repeat(64) }) });

    const result = await coverage("report", { raw, report });
    expect(result.code, describeRun(result)).toBe(1);
    expect(result.stderr).toContain("DWLOC_BIN");
    expect(result.stderr).toContain("node e2e/coverage.mjs clean");
    expect(existsSync(report)).toBe(false);
  });

  test("1回分の生データなら、集計して報告を書く", async ({}, testInfo) => {
    const raw = testInfo.outputPath("raw");
    const report = testInfo.outputPath("report");
    await writeTree(raw, {
      "a.json": rawBody("1000-aaaaaaaa"),
      "b.json": rawBody("1000-aaaaaaaa"),
    });

    const result = await coverage("report", { raw, report });
    expect(result.stdout, describeRun(result)).toContain("生データ 2 ファイル、2 件を合算しました");
    expect(existsSync(join(report, "index.html"))).toBe(true);
    expect(existsSync(join(report, "lcov.info"))).toBe(true);
  });
});
