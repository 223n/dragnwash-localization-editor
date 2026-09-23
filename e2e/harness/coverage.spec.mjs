// 試験の仕組みのうち、カバレッジの集計（coverage.mjs）と、その置き場の扱いを見る。
//
// 集計は子の node で `node e2e/coverage.mjs <命令>` として走らせ、置き場は DWLOC_E2E_RAW と
// DWLOC_E2E_REPORT で試験ごとの一時ディレクトリへ向ける。既定の置き場（coverage/）は、
// いま走っている実行が生データを書いている場所なので触らない。
//
// clean が実際に消す場合は、ここでは走らせない。clean は Playwright の出力の親
// （test-results/e2e）も消すので、いま走っている実行の出力まで消してしまう。
// 消してよいかの判断は、report と同じ処理（support/owned-dir.mjs）を通る。
import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { mkdir, readdir, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";

import { expect, test } from "@playwright/test";

import { binaryPath } from "../support/dwloc.mjs";
import { appJs } from "../support/paths.mjs";
import { childEnv, describeRun, e2eDir, moduleUrl, runNode, runScript } from "./node.mjs";

const source = readFileSync(appJs, "utf8");
const sourceHash = createHash("sha256").update(source, "utf8").digest("hex");

// 目印のファイル名（support/paths.mjs）。字面で持つのは、名前を変えると、前に作った
// 置き場を目印の無いディレクトリとして断るようになるからである。変えるなら、ここも変える。
const RAW_MARKER = ".dwloc-e2e-raw";
const REPORT_MARKER = ".dwloc-e2e-report";

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

// listTree は base の下のファイルを、base からの相対パス（/ 区切り）で並べて返す。
async function listTree(base) {
  const entries = await readdir(base, { recursive: true, withFileTypes: true });
  return entries
    .filter((entry) => entry.isFile())
    .map((entry) => join(entry.parentPath, entry.name).slice(base.length + 1).replaceAll("\\", "/"))
    .sort();
}

// globalSetup は試験の始めの処理（global-setup.mjs）を子の node で走らせる。ビルドは
// 省く（DWLOC_BIN にいま使っているバイナリを渡す）。
function globalSetup(raw) {
  return runScript(
    `
    import globalSetup from ${JSON.stringify(moduleUrl("global-setup.mjs"))};
    await globalSetup();
    `,
    { env: childEnv({ DWLOC_BIN: binaryPath(), DWLOC_E2E_RAW: raw }) },
  );
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

test.describe("置き場を環境変数で変えたとき", () => {
  test("DWLOC_E2E_REPORT が目印の無い、中身のあるディレクトリを指していたら、消さずに止める", async ({}, testInfo) => {
    // 報告は書く前に置き場を丸ごと消す。親の coverage/ を指しただけで、Go のカバー
    // プロファイルや生データまで消える。
    const raw = testInfo.outputPath("raw");
    const report = testInfo.outputPath("report");
    await writeTree(raw, { "run.json": rawBody("1000-aaaaaaaa") });
    const mine = { "notes.txt": "手元のメモ", "go/cover.out": "mode: set\n" };
    await writeTree(report, mine);

    const result = await coverage("report", { raw, report });
    expect(result.code, describeRun(result)).toBe(1);
    expect(result.stderr).toContain("DWLOC_E2E_REPORT");
    expect(result.stderr).toContain(REPORT_MARKER);
    expect(await listTree(report)).toEqual(Object.keys(mine).sort());
  });

  test("DWLOC_E2E_RAW が目印の無い、中身のあるディレクトリを指していたら、clean は何も消さずに止める", async ({}, testInfo) => {
    // 途中まで消してから止めると、消してよいものと消してはいけないものが半端に残る。
    // 先に全部を確かめる。消してよい報告の置き場も、ここでは残っていなければならない。
    const raw = testInfo.outputPath("raw");
    const report = testInfo.outputPath("report");
    const mine = { "notes.txt": "手元のメモ", "sub/keep.po": 'msgid ""\n' };
    await writeTree(raw, mine);
    await writeTree(report, { [REPORT_MARKER]: "", "index.html": "<!doctype html>" });

    const result = await coverage("clean", { raw, report });
    expect(result.code, describeRun(result)).toBe(1);
    expect(result.stderr).toContain("DWLOC_E2E_RAW");
    expect(result.stderr).toContain(RAW_MARKER);
    expect(await listTree(raw)).toEqual(Object.keys(mine).sort());
    expect(await listTree(report)).toEqual([REPORT_MARKER, "index.html"].sort());
  });

  test("空のディレクトリなら目印を置いて使い、目印のある置き場は丸ごと書き直す", async ({}, testInfo) => {
    const raw = testInfo.outputPath("raw");
    const report = testInfo.outputPath("report");
    await writeTree(raw, { "run.json": rawBody("1000-aaaaaaaa") });
    await mkdir(report, { recursive: true });

    const first = await coverage("report", { raw, report });
    expect(existsSync(join(report, "index.html")), describeRun(first)).toBe(true);
    expect(existsSync(join(report, "lcov.info"))).toBe(true);
    expect(existsSync(join(report, REPORT_MARKER))).toBe(true);

    // 前の報告の残り（消えたファイルの頁など）を持ち越さない。
    await writeFile(join(report, "stale.html"), "前の報告");
    const second = await coverage("report", { raw, report });
    expect(existsSync(join(report, "index.html")), describeRun(second)).toBe(true);
    expect(existsSync(join(report, REPORT_MARKER))).toBe(true);
    expect(existsSync(join(report, "stale.html"))).toBe(false);
  });

  test("生データの置き場が報告の置き場の中にあれば、止める", async ({}, testInfo) => {
    // 報告を書き直すと、読んだばかりの生データまで消える。
    const report = testInfo.outputPath("report");
    const raw = join(report, "raw");
    await writeTree(report, { [REPORT_MARKER]: "" });
    await writeTree(raw, { "run.json": rawBody("1000-aaaaaaaa") });

    const result = await coverage("report", { raw, report });
    expect(result.code, describeRun(result)).toBe(1);
    expect(result.stderr).toContain("DWLOC_E2E_RAW");
    expect(result.stderr).toContain("DWLOC_E2E_REPORT");
    expect(await listTree(raw)).toEqual(["run.json"]);
  });

  test("試験の始めに、目印の無い、中身のある DWLOC_E2E_RAW を断り、何も書き込まない", async ({}, testInfo) => {
    // 生データを書き込んでから clean で断ると、書き込んだ生データが手元のファイルに
    // 混ざったまま残る。試験を1つも走らせないうちに止める。
    const raw = testInfo.outputPath("raw");
    const mine = { "notes.txt": "手元のメモ" };
    await writeTree(raw, mine);

    const result = await globalSetup(raw);
    expect(result.code, describeRun(result)).not.toBe(0);
    expect(result.stderr).toContain("DWLOC_E2E_RAW");
    expect(await listTree(raw)).toEqual(Object.keys(mine));
  });

  test("試験の始めに、まだ無い DWLOC_E2E_RAW を作って目印を置く", async ({}, testInfo) => {
    // 目印を置いておかないと、あとの clean が、この試験の仕組みが作った置き場だと
    // 分からずに断る。
    const raw = testInfo.outputPath("raw");

    const result = await globalSetup(raw);
    expect(result.code, describeRun(result)).toBe(0);
    expect(await listTree(raw)).toEqual([RAW_MARKER]);
  });
});
