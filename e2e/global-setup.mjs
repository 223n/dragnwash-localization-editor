// 試験の前に dwloc を1回だけビルドする。
//
// 置き場は実行ごとに作る一時ディレクトリにする。リポジトリの中（/dwloc や dist/）に
// 作ると、同時に走る別の実行が同じファイルを書き換える。Windows では動いている
// 実行ファイルを上書きできないので、片方のビルドが落ちる。
//
// 環境変数 DWLOC_BIN があれば、ビルドせずにそれを使う。Linux のコンテナで、
// ホストがビルドしたバイナリを渡すための道である。相対パスはリポジトリのルートから
// 引く（support/paths.mjs の givenBinary）。
//
// ビルドしたパスは DWLOC_E2E_BIN でワーカーへ渡す。ワーカーは globalSetup の
// あとに起動し、ランナーの環境を受け継ぐ。返した関数が後始末になり、一時
// ディレクトリを消す。
//
// ビルドの前に、カバレッジの生データの置き場を確かめて目印を置く（support/owned-dir.mjs）。
// DWLOC_E2E_RAW が関係の無いファイルのあるディレクトリを指していたら、試験を1つも
// 走らせないうちに止める。生データを書き込んでから clean で断ると、書き込んだ生データが
// 手元のファイルに混ざったまま残る。
import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { claimPlace } from "./support/owned-dir.mjs";
import { givenBinary, rawPlace, root } from "./support/paths.mjs";

export default async function globalSetup() {
  await claimPlace(rawPlace());

  const given = givenBinary();
  if (given) {
    if (!existsSync(given)) {
      throw new Error(`DWLOC_BIN が指す dwloc がありません: ${given}（相対パスはリポジトリのルートから引きます）`);
    }
    return undefined;
  }

  const dir = await mkdtemp(join(tmpdir(), "dwloc-e2e-bin-"));
  const bin = join(dir, process.platform === "win32" ? "dwloc.exe" : "dwloc");
  const result = spawnSync("go", ["build", "-o", bin, "./cmd/dwloc"], {
    cwd: root,
    stdio: "inherit",
  });
  if (result.error || result.status !== 0) {
    await rm(dir, { recursive: true, force: true });
    const why = result.error ? result.error.message : `終了コード ${result.status}`;
    throw new Error(`dwloc をビルドできませんでした（${why}）`);
  }
  process.env.DWLOC_E2E_BIN = bin;

  return async () => {
    await rm(dir, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
  };
}
