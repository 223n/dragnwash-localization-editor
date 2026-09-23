// E2E の置き場をまとめる。設定、フィクスチャ、集計スクリプトが同じ場所を見るように、
// パスと環境変数の読み方はここにしか書かない。
//
// 環境変数:
//   DWLOC_BIN          使う dwloc の実行ファイル。あればビルドを省く（Linux のコンテナで
//                      ホストが作ったバイナリを使うため）。相対パスはリポジトリのルートから引く
//   DWLOC_E2E_RAW      カバレッジの生データの置き場（既定: coverage/e2e-raw）
//   DWLOC_E2E_REPORT   集計した報告（html と lcov）の置き場（既定: coverage/e2e）
//   DWLOC_E2E_OUTPUT   Playwright の outputDir（既定: test-results/e2e/<実行ごとの ID>）
//
// 生データも outputDir も、同時に走る別の実行とぶつからないようにしてある。
// 生データはファイル名に一意の ID を入れるので、同じ置き場を共有しても重ならない。
// outputDir は Playwright が実行の始めに中身を消すので、既定では実行ごとに別の
// ディレクトリにする。消す処理（coverage.mjs clean）はフル実行でしか呼ばない。
import { randomUUID } from "node:crypto";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// root はリポジトリのルート。
export const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

// appJs は画面のスクリプトそのもの。カバレッジはこのファイルへ対応づける。
export const appJs = join(root, "internal", "web", "ui", "app.js");

// givenBinary は DWLOC_BIN が指す dwloc を絶対パスで返す。指定が無ければ null。
//
// dwloc は見本の一時ディレクトリをカレントにして起動する（support/dwloc.mjs）。相対パスの
// まま渡すとそこから引かれ、ファイルはあるのに ENOENT で全部の試験が落ちる。
// 引く起点をランナーのカレントディレクトリではなくリポジトリのルートにするのは、
// npx playwright をどこから走らせても同じファイルを指すようにするためである
// （npm run はいつもルートで走るので、npm run test:e2e ではどちらでも同じになる）。
export function givenBinary() {
  const given = process.env.DWLOC_BIN;
  return given ? resolve(root, given) : null;
}

// reportDir は集計した報告（html と lcov）の置き場。
//
// 環境変数で変えられるのは、1つのスペックだけの数字を別の置き場で見るためである。
// 同時に走る別の集計と同じ場所へ書くと、報告が混ざる。
export const reportDir = process.env.DWLOC_E2E_REPORT
  ? resolve(process.env.DWLOC_E2E_REPORT)
  : join(root, "coverage", "e2e");

// outputBase は Playwright の出力をまとめる親。フル実行の前にここごと消す。
export const outputBase = join(root, "test-results", "e2e");

// rawDir はカバレッジの生データの置き場を返す。
export function rawDir() {
  const fromEnv = process.env.DWLOC_E2E_RAW;
  return fromEnv ? resolve(fromEnv) : join(root, "coverage", "e2e-raw");
}

// outputDir は Playwright の outputDir を返す。
//
// 実行ごとの ID は、最初に呼んだ（＝設定を読んだ）ランナーの環境変数に入れる。
// ワーカーはランナーの環境を受け継いで起動し、設定を読み直すので、同じ ID を見る。
// ここを呼ぶたびに ID を作ると、ランナーとワーカーで別の場所を指してしまう。
export function outputDir() {
  const fromEnv = process.env.DWLOC_E2E_OUTPUT;
  if (fromEnv) {
    return resolve(fromEnv);
  }
  if (!process.env.DWLOC_E2E_RUN) {
    process.env.DWLOC_E2E_RUN = `${Date.now()}-${randomUUID().slice(0, 8)}`;
  }
  return join(outputBase, process.env.DWLOC_E2E_RUN);
}
