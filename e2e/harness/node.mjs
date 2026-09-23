// 試験の仕組み（support/ と coverage.mjs）を、別の node で動かす。
//
// 確かめたいのは、環境変数（DWLOC_BIN や置き場の指定、一時ディレクトリの場所）と
// カレントディレクトリを変えたときの振る舞いである。ワーカーの中で環境を書き換えると、
// 同じワーカーで続けて走る試験まで巻き込む。捕まらない例外でワーカーごと落ちることも
// あるので、子の node に閉じ込める。
import { spawn } from "node:child_process";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

import { root } from "../support/paths.mjs";

// e2eDir は e2e/ のディレクトリ。子に読ませるスクリプトはここから引く。
export const e2eDir = join(root, "e2e");

// moduleUrl は import に渡す URL を作る。Windows のパス（C:\...）はそのままでは渡せない。
export function moduleUrl(...parts) {
  return pathToFileURL(join(e2eDir, ...parts)).href;
}

// childEnv は子に渡す環境を作る。
//
// ワーカーの環境から、試験の仕組みが読む変数を抜いてから overrides を重ねる。
// 抜かないと、ランナーに渡した DWLOC_BIN や置き場の指定が子に漏れ、確かめたい条件が
// 崩れる。GITHUB_STEP_SUMMARY も抜く。子の集計が CI のジョブの要約に表を足すからである。
export function childEnv(overrides = {}) {
  const env = { ...process.env };
  for (const name of [
    "DWLOC_BIN",
    "DWLOC_E2E_BIN",
    "DWLOC_E2E_RAW",
    "DWLOC_E2E_REPORT",
    "DWLOC_E2E_OUTPUT",
    "DWLOC_E2E_RUN",
    "GITHUB_STEP_SUMMARY",
  ]) {
    delete env[name];
  }
  return { ...env, ...overrides };
}

// tempEnv は一時ディレクトリの置き場を dir に向ける環境変数を返す。os.tmpdir() は
// Windows では TEMP と TMP を、POSIX では TMPDIR を見る。
export function tempEnv(dir) {
  return { TEMP: dir, TMP: dir, TMPDIR: dir };
}

// runNode は node を子として走らせ、終わるまで待つ。
//
// timeout を過ぎたら止めて、そのことを結果に残す。子が止まらないまま待ち続けると、
// 試験の時間切れの報告しか残らず、子が何を出していたかが分からない。
export function runNode(args, { cwd = root, env = childEnv(), timeout = 60_000 } = {}) {
  return new Promise((resolve, reject) => {
    const started = Date.now();
    const child = spawn(process.execPath, args, { cwd, env, stdio: ["ignore", "pipe", "pipe"], windowsHide: true });
    let stdout = "";
    let stderr = "";
    let timedOut = false;
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    const timer = setTimeout(() => {
      timedOut = true;
      child.kill("SIGKILL");
    }, timeout);
    child.on("error", (err) => {
      clearTimeout(timer);
      reject(err);
    });
    child.on("close", (code, signal) => {
      clearTimeout(timer);
      resolve({ code, signal, stdout, stderr, timedOut, elapsed: Date.now() - started });
    });
  });
}

// runScript は ESM のスクリプトを子の node で走らせる。
export function runScript(source, options) {
  return runNode(["--input-type=module", "--eval", source], options);
}

// describeRun は子の結果を、失敗したときに読める形にする。expect の説明に渡す。
export function describeRun(result) {
  return [
    `code=${result.code} signal=${result.signal} timedOut=${result.timedOut} elapsed=${result.elapsed}ms`,
    "--- stdout ---",
    result.stdout,
    "--- stderr ---",
    result.stderr,
  ].join("\n");
}
