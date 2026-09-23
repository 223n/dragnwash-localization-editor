// 画面（app.js）のカバレッジを集計し、package.json の閾値を下回っていたら失敗させる。
//
// 使い方:
//   node e2e/coverage.mjs clean    生データと前回の報告と Playwright の出力を消す（フル実行の前だけ）
//   node e2e/coverage.mjs report   生データを集計して報告を書き、閾値と比べる
//
// 生データは試験の page フィクスチャ（support/coverage.mjs）が、頁ごとに V8 の形で
// 書き出したものである。ここでそれを v8-to-istanbul で internal/web/ui/app.js に
// 対応づけ、istanbul の CoverageMap に合算する。
//
// 合算の前に、配られた app.js が手元の internal/web/ui/app.js と同じものかを確かめる。
// dwloc は画面を実行ファイルに埋め込んで配るので、ビルドが古い（DWLOC_BIN に前の
// バイナリを渡した、など）と、別の版のスクリプトの位置を今のファイルへ当てはめる
// ことになり、数字がでたらめになる。
//
// 数え方の注意（V8 のカバレッジを istanbul の形にしたときの性質）:
//   - 行と文は同じ数になる。v8-to-istanbul は1行を1つの文として数えるためで、
//     ここではコメントと空行だけの行を外してから数える（keepCodeLines）。
//   - 関数は名前のある関数だけを数える。v8-to-istanbul は名前の無い関数
//     （addEventListener に渡している function () {...}）を関数として数えない。
//   - 分岐は V8 が報告した塊の数で、一度も呼ばれなかった関数は中の if を報告しない
//     （関数全体の1つになる）。そのため分岐の率は実際より高めに出る。
//
// 閾値は package.json の coverageThresholds.e2e にある（lines / statements /
// functions / branches）。GITHUB_STEP_SUMMARY があれば、ジョブの要約に表を足す。
import { createHash } from "node:crypto";
import { appendFileSync, existsSync, readdirSync, readFileSync } from "node:fs";
import { mkdir, rm } from "node:fs/promises";
import { join, relative } from "node:path";

import libCoverage from "istanbul-lib-coverage";
import libReport from "istanbul-lib-report";
import reports from "istanbul-reports";
import v8toIstanbul from "v8-to-istanbul";

import { codeLines } from "./support/code-lines.mjs";
import { appJs, outputBase, rawDir, reportDir, root } from "./support/paths.mjs";

// metrics は比べる4つの指標。表と閾値の並びもこの順にする。
const metrics = ["lines", "statements", "functions", "branches"];

async function clean() {
  for (const dir of [rawDir(), reportDir, outputBase]) {
    await rm(dir, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
  }
}

function readThresholds() {
  const pkg = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
  const values = pkg.coverageThresholds?.e2e;
  if (!values) {
    throw new Error("package.json に coverageThresholds.e2e がありません");
  }
  for (const name of metrics) {
    if (typeof values[name] !== "number") {
      throw new Error(`package.json の coverageThresholds.e2e.${name} が数ではありません`);
    }
  }
  return values;
}

// readRaw は生データを全部読む。
function readRaw(dir) {
  if (!existsSync(dir)) {
    return [];
  }
  return readdirSync(dir)
    .filter((name) => name.endsWith(".json"))
    .sort()
    .map((name) => ({ name, body: JSON.parse(readFileSync(join(dir, name), "utf8")) }));
}

async function buildMap(raw, source) {
  const want = createHash("sha256").update(source, "utf8").digest("hex");
  const map = libCoverage.createCoverageMap({});
  let entries = 0;
  for (const { name, body } of raw) {
    for (const entry of body.entries ?? []) {
      if (entry.sourceHash !== want) {
        throw new Error(
          `配られた app.js が ${relative(root, appJs)} と違います（${name}、${body.test}）。` +
            "dwloc をビルドし直してください（DWLOC_BIN に古いバイナリを渡していないかも見てください）",
        );
      }
      // 変換器は1エントリごとに作り直す。applyCoverage は数を足さずに上書きするので、
      // 使い回すと前のエントリの数が消える。合算は CoverageMap の merge に任せる。
      const converter = v8toIstanbul(appJs, 0, { source });
      await converter.load();
      converter.applyCoverage(entry.functions);
      map.merge(converter.toIstanbul());
      entries++;
    }
  }
  return { map, entries };
}

// keepCodeLines は、空白とコメントしか無い行を文（＝行）の数から外す。
//
// v8-to-istanbul は1行を1つの文として数え、コメントの行も入れる（support/code-lines.mjs
// の注記）。外さないと、いつも実行される即時関数の直下にある長い注記が「通った行」に
// なり、数字が実際より大きく出る。関数と分岐の数には手を入れない。
function keepCodeLines(map, source) {
  const code = codeLines(source);
  let before = 0;
  let after = 0;
  for (const file of map.files()) {
    const data = map.fileCoverageFor(file).data;
    const statementMap = {};
    const hits = {};
    let next = 0;
    for (const [id, loc] of Object.entries(data.statementMap)) {
      before++;
      if (!code.has(loc.start.line)) {
        continue;
      }
      statementMap[next] = loc;
      hits[next] = data.s[id];
      next++;
    }
    after += next;
    data.statementMap = statementMap;
    data.s = hits;
  }
  return { before, after };
}

function writeReports(map) {
  const context = libReport.createContext({
    dir: reportDir,
    coverageMap: map,
    defaultSummarizer: "nested",
  });
  reports.create("text-summary").execute(context);
  reports.create("html").execute(context);
  reports.create("lcovonly", { file: "lcov.info" }).execute(context);
}

function writeStepSummary(summary, thresholds, files, entries) {
  const path = process.env.GITHUB_STEP_SUMMARY;
  if (!path) {
    return;
  }
  const rows = metrics.map((name) => {
    const m = summary[name];
    const mark = m.pct >= thresholds[name] ? "" : " **（閾値未満）**";
    return `| ${name} | ${m.pct.toFixed(2)}% | ${m.covered} / ${m.total} | ${thresholds[name]}%${mark} |`;
  });
  appendFileSync(
    path,
    [
      "### 画面（app.js）のカバレッジ",
      "",
      `生データ ${files} ファイル、${entries} 件を合算しました。`,
      "",
      "| 指標 | 率 | 通った / 全体 | 閾値 |",
      "| ---- | ---: | ---: | ---: |",
      ...rows,
      "",
      "",
    ].join("\n"),
  );
}

async function report() {
  const dir = rawDir();
  const raw = readRaw(dir);
  if (raw.length === 0) {
    console.error(`カバレッジの生データがありません: ${dir}`);
    console.error("先に playwright test -c e2e を走らせてください");
    process.exit(1);
  }

  const source = readFileSync(appJs, "utf8");
  const { map, entries } = await buildMap(raw, source);
  const lines = keepCodeLines(map, source);

  await rm(reportDir, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
  await mkdir(reportDir, { recursive: true });
  console.log(`生データ ${raw.length} ファイル、${entries} 件を合算しました（${relative(root, dir)}）`);
  console.log(`行と文は、コメントと空行を除いた ${lines.after} 行で数えています（全 ${lines.before} 行）`);
  writeReports(map);
  console.log(`報告: ${relative(root, join(reportDir, "index.html"))} / ${relative(root, join(reportDir, "lcov.info"))}`);

  const summary = map.getCoverageSummary().toJSON();
  const thresholds = readThresholds();
  writeStepSummary(summary, thresholds, raw.length, entries);

  const below = metrics.filter((name) => summary[name].pct < thresholds[name]);
  for (const name of metrics) {
    const m = summary[name];
    console.log(`${name.padEnd(10)} ${m.pct.toFixed(2).padStart(6)}%（閾値 ${thresholds[name]}%）`);
  }
  if (below.length > 0) {
    const detail = below.map((name) => `${name} ${summary[name].pct.toFixed(2)}% < ${thresholds[name]}%`);
    console.error(`画面のカバレッジが閾値を下回りました: ${detail.join(", ")}`);
    process.exit(1);
  }
}

const command = process.argv[2];
try {
  switch (command) {
    case "clean":
      await clean();
      break;
    case "report":
      await report();
      break;
    default:
      console.error("使い方: node e2e/coverage.mjs clean | report");
      process.exit(2);
  }
} catch (err) {
  // 集計できなかった理由だけを出す。スタックを出しても、直す場所はこのファイルではない。
  console.error(err.message);
  process.exit(1);
}
