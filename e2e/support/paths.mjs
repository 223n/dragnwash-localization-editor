// E2E の置き場をまとめる。設定、フィクスチャ、集計スクリプトが同じ場所を見るように、
// パスと環境変数の読み方はここにしか書かない。
//
// 環境変数:
//   DWLOC_BIN          使う dwloc の実行ファイル。あればビルドを省く（Linux のコンテナで
//                      ホストが作ったバイナリを使うため）。相対パスはリポジトリのルートから引く
//   DWLOC_E2E_RAW      カバレッジの生データの置き場（既定: coverage/e2e-raw）
//   DWLOC_E2E_REPORT   集計した報告（html と lcov）の置き場（既定: coverage/e2e）
//                      この2つは clean と report が丸ごと消す。まだ無いか空のディレクトリを
//                      指す。目印の無い、中身のあるディレクトリを指すと、消さずに止める
//   DWLOC_E2E_OUTPUT   Playwright の outputDir（既定: test-results/e2e/<実行ごとの ID>）。
//                      中身は Playwright が実行の始めに消す
//   DWLOC_E2E_RUN      実行ごとの ID。設定を読むときに作ってワーカーへ渡す（runId）。
//                      外から渡すと、別々の実行の生データを見分けられなくなる
//
// 生データも outputDir も、同時に走る別の実行とぶつからないようにしてある。
// 生データはファイル名に一意の ID を入れるので、同じ置き場を共有しても重ならない。
// ただし集計（coverage.mjs report）は、1回分の実行の生データしか数えない。前の実行の
// ぶんが残っていれば、数えずに止める。outputDir は Playwright が実行の始めに中身を
// 消すので、既定では実行ごとに別のディレクトリにする。消す処理（coverage.mjs clean）は
// フル実行でしか呼ばない。
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

// 置き場（place）は { dir, name, fromEnv, marker } で表す。dir はパス、name は置き場を
// 変える環境変数の名前（知らせるときに使う）、fromEnv は環境変数で変えているか、
// marker は目印のファイル名。
//
// 目印は、この試験の仕組みが置き場を使い始めるときに置く。clean と report は置き場を
// 丸ごと消すので、環境変数で変えた置き場は、目印があるか空のときだけ使う
// （support/owned-dir.mjs）。指した先に関係の無いファイルがあれば、消さずに止める。
function place(name, fallback, marker) {
  const value = process.env[name];
  return value
    ? { dir: resolve(value), name, fromEnv: true, marker }
    : { dir: fallback, name, fromEnv: false, marker };
}

// RAW_MARKER と REPORT_MARKER は目印のファイル名。置き場ごとに分けるのは、生データと
// 報告に同じディレクトリを指したとき、片方の目印でもう片方を消さないためである。
export const RAW_MARKER = ".dwloc-e2e-raw";
export const REPORT_MARKER = ".dwloc-e2e-report";

// reportPlace は集計した報告（html と lcov）の置き場。reportDir はそのパス。
//
// 環境変数で変えられるのは、1つのスペックだけの数字を別の置き場で見るためである。
// 同時に走る別の集計と同じ場所へ書くと、報告が混ざる。指す先は、報告のためだけの
// ディレクトリ（まだ無いか、空のもの）にする。
export const reportPlace = place("DWLOC_E2E_REPORT", join(root, "coverage", "e2e"), REPORT_MARKER);
export const reportDir = reportPlace.dir;

// outputBase は Playwright の出力をまとめる親。フル実行の前にここごと消す。
// 環境変数では変えられないので、目印は見ない。
export const outputBase = join(root, "test-results", "e2e");
export const outputPlace = { dir: outputBase, name: "Playwright の出力の置き場", fromEnv: false, marker: null };

// rawPlace はカバレッジの生データの置き場を返す。rawDir はそのパス。指す先は、生データの
// ためだけのディレクトリ（まだ無いか、空のもの）にする。
export function rawPlace() {
  return place("DWLOC_E2E_RAW", join(root, "coverage", "e2e-raw"), RAW_MARKER);
}

export function rawDir() {
  return rawPlace().dir;
}

// runId は実行ごとの ID（DWLOC_E2E_RUN）を返す。
//
// ID は、最初に呼んだ（＝設定を読んだ）ランナーの環境変数に入れる。ワーカーは
// ランナーの環境を受け継いで起動し、設定を読み直すので、同じ ID を見る。
// 呼ぶたびに ID を作ると、ランナーとワーカーで別の実行に見えてしまう。
//
// 使い道は2つある。Playwright の outputDir を実行ごとに分けることと、生データに
// どの実行のものかを書き込むこと（集計は、1回分の実行の生データだけを数える）。
export function runId() {
  if (!process.env.DWLOC_E2E_RUN) {
    process.env.DWLOC_E2E_RUN = `${Date.now()}-${randomUUID().slice(0, 8)}`;
  }
  return process.env.DWLOC_E2E_RUN;
}

// outputDir は Playwright の outputDir を返す。
//
// DWLOC_E2E_OUTPUT で置き場を変えたときも、実行ごとの ID はここで作っておく。
// 設定を読むときに作らないと、ワーカーがそれぞれ別の ID を作り、生データが別々の
// 実行のものに見える。
export function outputDir() {
  const id = runId();
  const fromEnv = process.env.DWLOC_E2E_OUTPUT;
  return fromEnv ? resolve(fromEnv) : join(outputBase, id);
}
