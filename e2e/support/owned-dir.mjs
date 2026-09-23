// 生データと報告の置き場を、この試験の仕組みが使い始めたものに限って消す。
//
// 置き場は環境変数（DWLOC_E2E_RAW と DWLOC_E2E_REPORT）で変えられる。clean と report は
// 置き場を丸ごと消すので、指した先を確かめずに消すと、親の coverage/ を指したり、
// 打ち間違えたりしただけで、関係の無いファイルまで消える。そこで、置き場を使い始める
// ときに目印のファイルを置き、環境変数で変えた置き場は、目印のあるものだけを消す。
// 目印の無いディレクトリでも、空なら使ってよい（目印を置く）。中身があれば、消しも
// 書き込みもせずに止める。
//
// 既定の置き場（coverage/ と test-results/ の下）は、目印が無くても消す。.gitignore で
// 試験の出力と決めてある場所で、目印を置くようになる前に作られたものも残っている。
//
// 置き場の形（dir、name、fromEnv、marker）は support/paths.mjs にある。
import { lstat, mkdir, readdir, rm, writeFile } from "node:fs/promises";
import { isAbsolute, join, relative, sep } from "node:path";

// note は目印のファイルに書く文。開いた人が、消してよいものかを判断できるようにする。
const note =
  "このディレクトリは dragnwash-localization-editor の E2E（e2e/coverage.mjs）の出力の置き場です。\n" +
  "node e2e/coverage.mjs clean と report が、中身ごと消します。\n";

// inspectPlace は置き場の様子を返す。"absent"（まだ無い）、"empty"（目印の無い空の
// ディレクトリ）、"owned"（消してよい）のどれか。使ってはいけない置き場なら投げる。
export async function inspectPlace(place) {
  let info;
  try {
    info = await lstat(place.dir);
  } catch (err) {
    if (err.code === "ENOENT") {
      return "absent";
    }
    throw err;
  }
  if (!place.fromEnv) {
    return "owned";
  }
  if (!info.isDirectory()) {
    throw new Error(
      `${place.name} が指す ${place.dir} はディレクトリではありません。` +
        "試験の出力だけを置く、空のディレクトリか、まだ無いディレクトリを指してください",
    );
  }
  const names = await readdir(place.dir);
  if (names.includes(place.marker)) {
    return "owned";
  }
  if (names.length === 0) {
    return "empty";
  }
  throw new Error(
    `${place.name} が指す ${place.dir} には、試験の仕組みが置く目印（${place.marker}）が無く、ほかのファイルがあります。` +
      "この置き場は丸ごと消すことがあるので、使わずに止めます。" +
      "試験の出力だけを置く、空のディレクトリか、まだ無いディレクトリを指してください",
  );
}

// claimPlace は置き場を確かめてから作り、目印を置く。すでに目印があれば、そのままにする。
export async function claimPlace(place) {
  await inspectPlace(place);
  await mkdir(place.dir, { recursive: true });
  if (!place.marker) {
    return;
  }
  try {
    // 並べて走らせた別の実行が同時に置いても壊れないように、無いときだけ作る。
    await writeFile(join(place.dir, place.marker), note, { flag: "wx" });
  } catch (err) {
    if (err.code !== "EEXIST") {
      throw err;
    }
  }
}

// removePlace は、消してよい置き場だけを消す。目印の無い空のディレクトリは、指した人が
// 作ったものなので残す。
export async function removePlace(place) {
  if ((await inspectPlace(place)) === "owned") {
    await rm(place.dir, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
  }
}

// contains は child が parent そのものか、その下にあるかを返す。
function contains(parent, child) {
  const rel = relative(parent, child);
  return rel === "" || (rel !== ".." && !rel.startsWith(`..${sep}`) && !isAbsolute(rel));
}

// assertApart は2つの置き場が重なっていないことを確かめる。一方がもう一方の中にあると、
// 外側を消したときに内側も消える（報告を書き直すと、読んだばかりの生データまで消える）。
export function assertApart(a, b) {
  if (contains(a.dir, b.dir) || contains(b.dir, a.dir)) {
    throw new Error(
      `${label(a)}（${a.dir}）と ${label(b)}（${b.dir}）が重なっています。` +
        "一方を消すと、もう一方も消えます。別々のディレクトリを指してください",
    );
  }
}

// label は知らせるときの置き場の呼び名。環境変数で変えていなければ、そう分かるようにする。
function label(place) {
  return place.fromEnv ? place.name : `${place.name} の既定の置き場`;
}
