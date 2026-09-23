// README に載せる画面の例を、見本のリポジトリ（samples/harbor）で撮り直す。
//
// 使い方（リポジトリのルートで）:
//   npm run screenshots                      docs/images/ へ書く
//   npm run screenshots -- <出力先>          別の場所へ書く
//
// 前もって npm ci と npx playwright install chromium が要る（E2E と同じ）。
// DWLOC_BIN にビルド済みの dwloc を渡すと、ビルドを省く。
//
// 見本は一時ディレクトリへ写してから開く。書き換えや競合の場面でファイルを書き換える
// ので、samples/harbor そのものを開くと見本が変わってしまう。起動と後始末は E2E の
// 仕組み（e2e/support/dwloc.mjs の launchDwloc）をそのまま使う。
//
// 撮る場面は4つで、画面の言語ごと（ja と en）に撮る。
//   edit-overview-<言語>.png   開いた直後の全体
//   edit-editing-<言語>.png    訳の欄を開いて打ったところ（未保存 1 件）
//   edit-conflict-<言語>.png   打っているあいだによそが同じ行を書き換えた（競合の引き止め）
//   edit-export-<言語>.png     上の帯の「別に保存」のメニュー
//
// 同じ見本を撮り直しても、画像のバイトは揃わないことがある。左の列の見出しの文字と
// 表の見出しの下の線が、1画素に満たない幅でずれる（Chromium の文字の描き方による。
// フォントの読み込みを待っても、文字の位置合わせを止めても揃わなかった）。目では
// 見分けられないので、撮り直した画像は、場面の中身が変わったときだけコミットする。
//
// 見本の台詞は、この説明のために作った架空のものである。ゲームの台本を再配布しない
// 方針なので、画面の例にゲームの台本を映してはいけない。
import { spawnSync } from "node:child_process";
import { mkdtemp, mkdir, readdir, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";

import { chromium } from "@playwright/test";

import { launchDwloc } from "../e2e/support/dwloc.mjs";

const root = fileURLToPath(new URL("..", import.meta.url));
const sample = join(root, "samples", "harbor");
const out = process.argv[2] ?? join(root, "docs", "images");

// 書き換えと競合の場面で使う行。物理行の番号はヘッダーが1行目。
const workingRel = "Translations/_discovered/ja.working.csv";
const wheels = { line: 10, source: "Don't forget the wheels." };
const typed = "ホイールも忘れずにね。";
const theirs = "タイヤまわりもお願い。";

// readTree は見本のファイルを { 相対パス: 中身 } にまとめる。launchDwloc はこの形で受け取る。
async function readTree(dir) {
  const files = {};
  for (const entry of await readdir(dir, { recursive: true, withFileTypes: true })) {
    if (!entry.isFile()) {
      continue;
    }
    const path = join(entry.parentPath, entry.name);
    files[relative(dir, path).split(sep).join("/")] = await readFile(path);
  }
  return files;
}

// buildDwloc は dwloc を一時ディレクトリへビルドし、そのパスを DWLOC_BIN に入れる。
async function buildDwloc() {
  if (process.env.DWLOC_BIN) {
    return null;
  }
  const dir = await mkdtemp(join(tmpdir(), "dwloc-shots-bin-"));
  const bin = join(dir, process.platform === "win32" ? "dwloc.exe" : "dwloc");
  const result = spawnSync("go", ["build", "-o", bin, "./cmd/dwloc"], { cwd: root, stdio: "inherit" });
  if (result.error || result.status !== 0) {
    await rm(dir, { recursive: true, force: true });
    throw new Error("dwloc をビルドできませんでした");
  }
  process.env.DWLOC_BIN = bin;
  return dir;
}

const files = await readTree(sample);
const working = files[workingRel].toString("utf8");
// よそが同じ行に書いた訳。見本の作業コピーで、その行の訳（空）を埋めた中身にする。
const external = working.replace(`,${wheels.source},\n`, `,${wheels.source},${theirs}\n`);
if (external === working) {
  throw new Error(`${workingRel} に「${wheels.source}」の訳の空いた行がありません`);
}

const binDir = await buildDwloc();
await mkdir(out, { recursive: true });
const browser = await chromium.launch();

// open は見本を写して dwloc edit を起動し、開き終えた頁を返す。
//
// 自動保存の時計は止めておく。止めないと、打った直後の「未保存」が撮る前に保存済みになる。
async function open(uiLang) {
  const server = await launchDwloc({ root: files, game: null }, { uiLang, locale: "ja" });
  const context = await browser.newContext({
    viewport: { width: 1280, height: 800 },
    deviceScaleFactor: 1,
    colorScheme: "light",
    locale: uiLang === "ja" ? "ja-JP" : "en-US",
  });
  const page = await context.newPage();
  await page.clock.install();
  await page.goto(server.url);
  await page.locator("#save-state").filter({ hasText: /\S/ }).waitFor();
  await page.locator("#list .row").first().waitFor();
  const now = await page.evaluate(() => Date.now());
  await page.clock.pauseAt(now + 1_000);
  return {
    page,
    server,
    async close() {
      await context.close();
      await server.stop();
    },
  };
}

// shot は頁を撮る。キャレットは点滅するので隠し、ポインターは画面の隅へ退ける。
async function shot(page, name) {
  await page.mouse.move(0, 0);
  await page.screenshot({ path: join(out, name), caret: "hide", animations: "disabled" });
  console.log(join(out, name));
}

async function typeIntoWheels(page) {
  await page.locator(`#list .cell.translation[data-line="${wheels.line}"]`).click();
  await page.locator("textarea.editor").fill(typed);
}

try {
  for (const lang of ["ja", "en"]) {
    let s = await open(lang);
    await shot(s.page, `edit-overview-${lang}.png`);
    await s.close();

    s = await open(lang);
    await typeIntoWheels(s.page);
    await shot(s.page, `edit-editing-${lang}.png`);
    await s.close();

    // 欄から離れると、その場で送る。版が合わないので 409 になり、引き止めが出る。
    s = await open(lang);
    await typeIntoWheels(s.page);
    await s.server.writeRoot(workingRel, external);
    await s.page.locator("textarea.editor").press("Escape");
    await s.page.locator("#conflict").waitFor({ state: "visible" });
    await shot(s.page, `edit-conflict-${lang}.png`);
    await s.close();

    s = await open(lang);
    await s.page.locator("#export-open").click();
    await s.page.locator("#export-working").waitFor({ state: "visible" });
    await shot(s.page, `edit-export-${lang}.png`);
    await s.close();
  }
} finally {
  await browser.close();
  if (binDir) {
    await rm(binDir, { recursive: true, force: true });
  }
}
