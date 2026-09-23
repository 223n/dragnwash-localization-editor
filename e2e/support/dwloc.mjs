// dwloc edit を起動して止める。
//
// 1回の起動ごとに一時ディレクトリを1つ作り、その中に見本と作業ディレクトリを置く。
//
//   <一時>/            作業ディレクトリ（dwloc はここに logs/ を作る）
//   <一時>/repo/       --root に渡す翻訳リポジトリ
//   <一時>/game/BepInEx/plugins/DragNWashLocalization/
//                      --game に渡すプラグインのフォルダー（見本に game があるときだけ）
//
// 作業ディレクトリを一時ディレクトリにするのは、dwloc が logs/dwloc_<日付>.log を
// カレントディレクトリに書くためである。リポジトリで起動すると、試験のたびに
// リポジトリの logs/ が伸びる。
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { chmod, lstat, mkdir, mkdtemp, readdir, readFile, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, isAbsolute, join, normalize, sep } from "node:path";

import { givenBinary } from "./paths.mjs";

// DEFAULT_OPTIONS は起動の既定値。test.use({ dwloc: { ... } }) で渡したものが上に重なる。
//
//   uiLang       --ui-lang（ja / en）。空なら付けない（Accept-Language で決まる）
//   locale       --locale。空なら付けない（画面でロケールを選ぶ既定の経路になる）
//   idleTimeout  --idle-timeout。既定の 0 は「自分では終わらない」。試験の途中で
//                待ち受けが消えないようにする
//   args         末尾に足す引数（--verbose など）
export const DEFAULT_OPTIONS = {
  uiLang: "ja",
  locale: "ja",
  idleTimeout: "0",
  args: [],
};

// startTimeout は URL が出るまで待つ上限。起動時に全ロケールを突き合わせるが、
// 見本は小さいので1秒もかからない。長めに取るのは CI の遅いランナーのため。
const startTimeout = 20_000;

// stopTimeout は止めてから終わるのを待つ上限。過ぎたら SIGKILL を送る。
const stopTimeout = 5_000;

// urlPattern は標準出力からトークン付きの URL を拾う。束ねる先は 127.0.0.1 に
// 固定されている（internal/web の listenHost）。
const urlPattern = /http:\/\/127\.0\.0\.1:(\d+)\/\?t=([A-Za-z0-9_-]+)/;

// running はこのワーカーで動いている待ち受け。ワーカーが後始末を飛ばして
// 終わるときにも止めるために持つ。--idle-timeout 0 の待ち受けは、止めないと
// 自分では終わらない。
const running = new Set();
process.on("exit", () => {
  for (const child of running) {
    try {
      child.kill();
    } catch {
      // 止められなくても続ける。終わりかけのプロセスで投げると残りを止められない。
    }
  }
});

// binaryPath は使う dwloc を絶対パスで返す。DWLOC_BIN が最優先で、無ければ globalSetup が
// ビルドしたもの（DWLOC_E2E_BIN）。dwloc は見本の一時ディレクトリをカレントにして
// 起動するので、相対パスのままでは返さない（support/paths.mjs の givenBinary）。
export function binaryPath() {
  const bin = givenBinary() || process.env.DWLOC_E2E_BIN;
  if (!bin) {
    throw new Error("dwloc のバイナリがありません。playwright test -c e2e で走らせてください（globalSetup がビルドします）");
  }
  if (!existsSync(bin)) {
    throw new Error(`dwloc のバイナリが見つかりません: ${bin}`);
  }
  return bin;
}

// safeJoin は見本の相対パスを base の下へつなぐ。外へ出るパスは断る。
function safeJoin(base, rel) {
  if (isAbsolute(rel)) {
    throw new Error(`見本のパスは相対で書いてください: ${rel}`);
  }
  const full = normalize(join(base, rel));
  if (full !== base && !full.startsWith(base + sep)) {
    throw new Error(`見本のパスが置き場の外を指しています: ${rel}`);
  }
  return full;
}

// writeTree は見本のファイルを書く。文字列は UTF-8 のバイトとしてそのまま書く。
async function writeTree(base, files) {
  for (const [rel, body] of Object.entries(files ?? {})) {
    const path = safeJoin(base, rel);
    await mkdir(dirname(path), { recursive: true });
    await writeFile(path, typeof body === "string" ? Buffer.from(body, "utf8") : body);
  }
}

// buildArgs は dwloc edit の引数を組む。
function buildArgs(root, game, opt) {
  const args = ["edit", "--root", root];
  if (game) {
    args.push("--game", game);
  } else {
    args.push("--no-game");
  }
  args.push("--no-browser", "--port", "0", "--idle-timeout", String(opt.idleTimeout));
  if (opt.uiLang) {
    args.push("--ui-lang", opt.uiLang);
  }
  if (opt.locale) {
    args.push("--locale", opt.locale);
  }
  args.push(...(opt.args ?? []));
  return args;
}

// spawnFailure は起動できなかった理由を、渡したファイルが分かる形にする。Node の理由には
// ファイルが出ないことがある（Windows で実行できないファイルを渡したときの「spawn EFTYPE」）。
function spawnFailure(bin, err) {
  return new Error(`dwloc を起動できませんでした（${bin}）: ${err.message}`, { cause: err });
}

// waitForUrl は標準出力に URL が出るまで待つ。出る前に終わったか、起動できなかったら
// 理由ごと投げる。どちらも exited（launchDwloc）で分かる。
function waitForUrl(child, out, exited, bin) {
  return new Promise((resolve, reject) => {
    let settled = false;
    const finish = (err, m) => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      child.stdout.off("data", onData);
      if (err) {
        reject(err);
      } else {
        resolve(m);
      }
    };
    const timer = setTimeout(() => {
      finish(new Error(`dwloc edit が ${startTimeout}ms 以内に URL を出しませんでした\n${out.text()}`));
    }, startTimeout);
    const onData = () => {
      const m = urlPattern.exec(out.stdout);
      if (m) {
        finish(null, m);
      }
    };
    child.stdout.on("data", onData);
    exited.then(({ code, signal, error }) => {
      if (error) {
        finish(spawnFailure(bin, error));
      } else {
        finish(new Error(`dwloc edit が URL を出す前に終わりました（code=${code} signal=${signal}）\n${out.text()}`));
      }
    });
    onData();
  });
}

// removeDir は見本の一時ディレクトリを消す。Windows はプロセスが終わった直後だと
// ファイルの手放しが遅れることがあるので、何度か試す。
//
// 書き込み権の無いディレクトリ（試験が 0555 にしたもの）が残っていると、POSIX では中身を
// 消せずに EACCES になる。Node の rm はこれを試し直さないので、権限を開けてから消し直す。
// 試験が戻し忘れたときや、戻す前に時間切れになったときの立て直しである。
async function removeDir(dir) {
  const options = { recursive: true, force: true, maxRetries: 10, retryDelay: 100 };
  try {
    await rm(dir, options);
  } catch (err) {
    if (err.code !== "EACCES" && err.code !== "EPERM") {
      throw err;
    }
    // 開けられなくても消し直しは試す。消せなかったときは、その理由を投げる。
    await openUp(dir).catch(() => {});
    await rm(dir, options);
  }
}

// openUp は path の下のディレクトリに持ち主の読み書きと入る権限を足し、ファイルには
// 持ち主の書き込み権を足す。シンボリックリンクはたどらない（見本の外を書き換えない）。
async function openUp(path) {
  const info = await lstat(path);
  if (info.isFile() && (info.mode & 0o200) === 0) {
    await chmod(path, (info.mode & 0o7777) | 0o200);
  }
  if (!info.isDirectory()) {
    return;
  }
  await chmod(path, (info.mode & 0o7777) | 0o700);
  for (const name of await readdir(path)) {
    await openUp(join(path, name));
  }
}

// launchDwloc は見本を作って dwloc edit を起動し、URL が出たところで返す。
//
// repo は見本（repo.mjs の形）、options は DEFAULT_OPTIONS に重ねる起動の指定。
// 返したハンドルの stop() で止めて一時ディレクトリを消す。stop は何度呼んでもよい。
// 消す前に戻したいもの（書けなくしたディレクトリの権限など）は beforeRemove で頼む。
export async function launchDwloc(repo, options = {}) {
  const opt = { ...DEFAULT_OPTIONS, ...options };
  // 一時ディレクトリを作る前に確かめる。作ってから投げると、それが残る。
  const bin = binaryPath();
  // 実パスにしておく。dwloc は --game の値のシンボリックリンクを解く（internal/gamedir の
  // canonical）ので、macOS の /var（実体は /private/var）のままだと、画面に出るパスと
  // ハンドルのパスが食い違う。
  const dir = await realpath(await mkdtemp(join(tmpdir(), "dwloc-e2e-")));
  const root = join(dir, "repo");
  let game = null;
  let args;
  let child;
  try {
    await mkdir(join(root, "Translations"), { recursive: true });
    await writeTree(root, repo?.root);

    if (repo?.game) {
      game = join(dir, "game", "BepInEx", "plugins", "DragNWashLocalization");
      // 目印（Translations/_discovered）が無いと --game が断られる。
      await mkdir(join(game, "Translations", "_discovered"), { recursive: true });
      await writeTree(game, repo.game);
    }

    args = buildArgs(root, game, opt);
    try {
      child = spawn(bin, args, {
        cwd: dir,
        stdio: ["ignore", "pipe", "pipe"],
        windowsHide: true,
      });
    } catch (err) {
      throw spawnFailure(bin, err);
    }
  } catch (err) {
    // 見本を書きかけたところや、spawn がその場で断ったところ（Windows で実行できない
    // ファイルを渡したときの EFTYPE など）で投げても、一時ディレクトリを残さない。
    await removeDir(dir);
    throw err;
  }
  running.add(child);

  // 出力は読み続ける。読まずにおくと、管の容量が埋まったところで dwloc が書けずに止まる。
  const out = {
    stdout: "",
    stderr: "",
    text() {
      return `--- stdout ---\n${this.stdout}\n--- stderr ---\n${this.stderr}`;
    },
  };
  child.stdout.setEncoding("utf8");
  child.stderr.setEncoding("utf8");
  child.stdout.on("data", (chunk) => {
    out.stdout += chunk;
  });
  child.stderr.on("data", (chunk) => {
    out.stderr += chunk;
  });
  // exited は終わったときに { code, signal } で解ける。起動できなかったときは
  // { code: null, signal: null, error } で解ける。
  //
  // 起動の失敗（見つからない、実行できない）は 'exit' ではなく 'error' で届く。受け手が
  // 無いと捕まらない例外でワーカーごと落ち、URL を待つ側は startTimeout まで待ち続ける。
  // 受けるのは起動する前（'spawn' より前）だけにする。起動したあとの 'error' は止められ
  // なかったとき（kill の失敗）で、これまでどおり stop から投げさせる。
  let ended = false;
  const exited = new Promise((resolve) => {
    const onSpawnError = (error) => {
      ended = true;
      running.delete(child);
      resolve({ code: null, signal: null, error });
    };
    child.once("error", onSpawnError);
    child.once("spawn", () => child.off("error", onSpawnError));
    child.on("exit", (code, signal) => {
      ended = true;
      running.delete(child);
      resolve({ code, signal });
    });
  });

  // beforeRemove で頼まれた後始末。stop が待ち受けを止めたあと、一時ディレクトリを
  // 消す前に、頼まれたのと逆の順で呼ぶ。
  const cleanups = [];

  let stopped = false;
  const stop = async () => {
    if (stopped) {
      return;
    }
    stopped = true;
    if (!ended) {
      // Windows では TerminateProcess、Linux と macOS では SIGTERM になる。
      // dwloc は SIGTERM を受けないが、Go の既定の動きで終わる。
      child.kill();
      // 待ちの時計は止めておく。残すと、止め終えたあとも node が stopTimeout まで終わらない。
      let timer;
      const done = await Promise.race([
        exited,
        new Promise((resolve) => {
          timer = setTimeout(() => resolve(null), stopTimeout);
        }),
      ]);
      clearTimeout(timer);
      if (done === null) {
        child.kill("SIGKILL");
        await exited;
      }
    }
    // 後始末が投げても、残りの後始末と一時ディレクトリの削除は続ける。戻せなかった
    // ことは、消したあとで投げて知らせる。
    let failure = null;
    for (const cleanup of cleanups.reverse()) {
      try {
        await cleanup();
      } catch (err) {
        failure ??= err;
      }
    }
    await removeDir(dir);
    if (failure) {
      throw failure;
    }
  };

  let match;
  try {
    match = await waitForUrl(child, out, exited, bin);
  } catch (err) {
    await stop();
    throw err;
  }

  const port = Number(match[1]);
  const origin = `http://127.0.0.1:${port}`;
  const rootPath = (rel = "") => safeJoin(root, rel);
  const gamePath = (rel = "") => {
    if (!game) {
      throw new Error("この見本にはゲームのフォルダーがありません（repo.game が null）");
    }
    return safeJoin(game, rel);
  };

  return {
    // url はトークン付きの URL。最初にこれを開くと Cookie が置かれる。
    url: match[0],
    origin,
    port,
    token: match[2],
    // dir は作業ディレクトリ（一時ディレクトリそのもの）。
    dir,
    // root と game は絶対パス。game は見本に game が無ければ null。
    root,
    game,
    args,
    options: opt,
    rootPath,
    gamePath,
    // read* はバイト列を返す。見本と1バイトずつ比べるときはこちらを使う。
    readRoot: (rel) => readFile(rootPath(rel)),
    readGame: (rel) => readFile(gamePath(rel)),
    readRootText: (rel) => readFile(rootPath(rel), "utf8"),
    readGameText: (rel) => readFile(gamePath(rel), "utf8"),
    // write* は「よそが書き換えた」を作るためにある（409 の試験など）。
    writeRoot: (rel, body) => writeTree(root, { [rel]: body }),
    writeGame: (rel, body) => {
      gamePath(rel);
      return writeTree(game, { [rel]: body });
    },
    // stdout と stderr は、ここまでに出たぶん。
    stdout: () => out.stdout,
    stderr: () => out.stderr,
    // logText は logs/ の下にあるログファイルを全部つないで返す。原文と訳が
    // 記録に出ていないことを確かめる試験のためにある。
    logText: async () => {
      const logs = join(dir, "logs");
      if (!existsSync(logs)) {
        return "";
      }
      const names = (await readdir(logs)).sort();
      const parts = await Promise.all(names.map((name) => readFile(join(logs, name), "utf8")));
      return parts.join("");
    },
    // exited は終わったときに { code, signal } で解ける。
    exited,
    stop,
    // beforeRemove は、stop が一時ディレクトリを消す前に呼ぶ後始末を頼む。試験が書けなく
    // したディレクトリの権限を戻す、などに使う。試験の finally だけで戻すと、試験が
    // 途中で止まったまま時間切れになったとき、Playwright は finally より先にフィクスチャ
    // （test.mjs の launch）を畳むので、閉じたままのディレクトリを消しにいってしまう。
    beforeRemove: (cleanup) => {
      cleanups.push(cleanup);
    },
  };
}
