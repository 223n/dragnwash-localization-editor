// 頁ごとに app.js のカバレッジを取り、生データとして書き出す。
//
// 集めるのは V8 の形（Profiler の precise coverage）のままで、istanbul の形への
// 変換は集計（e2e/coverage.mjs report）でまとめて行う。試験の最中に変換すると、
// 1頁ごとに app.js（約2800行）を読み直すことになる。
//
// 書き出すのは URL が /app.js のエントリだけにする。ポートは試験ごとに違うので、
// URL はパスだけに縮めて持つ。中身は持たず SHA-256 だけを残し、集計の側で
// internal/web/ui/app.js と同じものかを確かめる（配っている app.js が埋め込みの
// ままであることの確かめ）。
import { createHash, randomUUID } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { rawDir } from "./paths.mjs";

// servedPath は取り出す資産のパス。
const servedPath = "/app.js";

// startCoverage は頁のカバレッジを取り始める。移動より前に呼ぶこと。
//
// resetOnNavigation を切るのは、最初の移動（/?t=<トークン> から / への 303）と、
// 試験の中の読み直しでカバレッジを捨てないためである。ただし Playwright の文書に
// あるとおり、切っても移動をまたいで残ることは保証されない。
export async function startCoverage(page) {
  await page.coverage.startJSCoverage({ resetOnNavigation: false });
}

// fileName は生データのファイル名を作る。並行に走る別の実行とも重ならないように、
// 最後に乱数の ID を付ける。題名は日本語なので名前には入れず、中身（test）に持たせる。
function fileName(testInfo, label) {
  const tag = label.replace(/[^A-Za-z0-9_-]+/g, "_");
  return `${testInfo.testId}-r${testInfo.repeatEachIndex}-${tag}-${randomUUID()}.json`;
}

// saveCoverage は頁のカバレッジを止めて、app.js のぶんを生データとして書く。
//
// 頁が閉じていたら何もしない（閉じた頁からは取り出せない）。取り出しに失敗しても
// 試験は落とさない。カバレッジは試験の結果ではなく、試験の副産物だからである。
// 失敗したことは試験の注記に残す。
export async function saveCoverage(page, testInfo, label = "page") {
  if (page.isClosed()) {
    testInfo.annotations.push({ type: "coverage", description: `${label}: 頁が閉じていたので取れませんでした` });
    return;
  }
  let entries;
  try {
    entries = await page.coverage.stopJSCoverage();
  } catch (err) {
    testInfo.annotations.push({ type: "coverage", description: `${label}: 取れませんでした（${err.message}）` });
    return;
  }
  const kept = [];
  for (const entry of entries) {
    let path;
    try {
      path = new URL(entry.url).pathname;
    } catch {
      continue;
    }
    if (path !== servedPath) {
      continue;
    }
    kept.push({
      url: path,
      sourceHash: createHash("sha256").update(entry.source ?? "", "utf8").digest("hex"),
      functions: entry.functions,
    });
  }
  if (kept.length === 0) {
    return;
  }
  const dir = rawDir();
  await mkdir(dir, { recursive: true });
  const body = {
    test: testInfo.titlePath.join(" > "),
    file: testInfo.file,
    entries: kept,
  };
  await writeFile(join(dir, fileName(testInfo, label)), JSON.stringify(body));
}
