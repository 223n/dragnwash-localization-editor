// 翻訳リポジトリの見本（フィクスチャ）を組む。
//
// 見本はファイルに置かず、ここでコードから作る。.gitattributes が「* text=auto eol=lf」
// なので、CRLF や BOM のようにバイト単位で意味のある見本をリポジトリに置くと、
// 取り出したときに改行が変わる。コードで作れば、書いたバイトがそのまま渡る。
//
// 形は Go の試験（internal/web の newTestRoot / newEditRoot / newTestGame）と
// internal/publish の target.go に合わせてある。
//
//   <ルート>/data/script_order.csv                         再生順
//   <ルート>/Translations/<ロケール>/strings.csv            公開ファイル（コミットする側）
//   <ルート>/Translations/_discovered/<ロケール>.working.csv 作業コピー（リポジトリ側）
//   <ゲーム>/Translations/_discovered/<ロケール>.working.csv 作業コピー（ゲーム側。先に当たる）
//   <ゲーム>/Translations/<ロケール>/strings.csv            ゲームに入っている公開ファイル（土台）
//
// <ゲーム> はプラグインのフォルダーで、目印の Translations/_discovered を持つ
// （internal/gamedir の hasDiscovered）。ハーネスが空でも作るので、見本に
// 作業コピーが無くても --game は通る。
//
// 見本の形（test.use({ repo }) に渡すもの）:
//
//   { root: { "相対パス": 中身, ... }, game: null | { "相対パス": 中身, ... } }
//
// 中身は文字列（UTF-8 のバイトとしてそのまま書く。改行も変えない）か Buffer。
// game が null なら --no-game で起動し、オブジェクトなら --game <プラグイン> で起動する。
import { createHash } from "node:crypto";

// ヘッダー。受理されるのは internal/edit の AcceptedHeaders の4種で、
// 作業コピーはゲーム内のModが書く7列、公開ファイルは publish が書く6列。
export const HEADER = {
  published: "key,section,node,order,speaker,translation",
  working: "key,section,node,order,speaker,source_en,translation",
  order: "section,phase,node,order,line_id,key,speaker,condition",
};

// BOM は UTF-8 の BOM。internal/edit は保存しても先頭に戻す。
export const BOM = "﻿";

// keyFor は原文からキーを作る（internal/key の For と同じ。SHA-256 の先頭8バイト）。
//
// キーを手で書かないのは、手で書いた16桁が再生順と食い違うと、publish で
// 捨てられる行や「台本から消えた行」に化けて、見たい振る舞いとは別の判定が付くため。
export function keyFor(source) {
  return createHash("sha256").update(source, "utf8").digest("hex").slice(0, 16);
}

// field は CSV の1フィールドを書く。引用が要るときだけ引用する。
//
// 前後に空白がある値も引用する。internal/edit の escapeTranslation と同じで、
// 引用しないと読み手（publish の ConvertFrom-Csv の移植）が空白を削る。
export function field(value) {
  const text = value === undefined || value === null ? "" : String(value);
  if (/[",\r\n]/.test(text) || /^\s|\s$/.test(text)) {
    return `"${text.replaceAll('"', '""')}"`;
  }
  return text;
}

// record はフィールドを並べて1行にする（改行は付けない）。
export function record(...fields) {
  return fields.map(field).join(",");
}

// joinLines は行をつなぐ。eol は文字列か、行の添字（0始まり）から改行を返す関数。
//
// 末尾にも改行を付ける。付けたくないときは trailing: false を渡す。
function joinLines(lines, { eol = "\n", trailing = true } = {}) {
  const term = typeof eol === "function" ? eol : () => eol;
  let out = "";
  lines.forEach((line, i) => {
    const last = i === lines.length - 1;
    out += line + (last && !trailing ? "" : term(i));
  });
  return out;
}

// dataLine は作業コピーか公開ファイルの1行を組む。
//
// item が文字列ならそのまま（見出しの "# ..." や空行 ""）。オブジェクトなら
// { source, translation, speaker, section, node, order, key } から組む。key を
// 省くと source から作る。
function dataLine(item, withSource) {
  if (typeof item === "string") {
    return item;
  }
  const key = item.key ?? keyFor(item.source ?? "");
  const head = [key, item.section ?? "L01 Ryan", item.node ?? "Ryan_1_intro", item.order ?? "", item.speaker ?? ""];
  if (withSource) {
    return record(...head, item.source ?? "", item.translation ?? "");
  }
  return record(...head, item.translation ?? "");
}

// workingCopy は作業コピーの中身を組む。1行目がヘッダーなので、items[i] は
// 物理行 i + 2 になる。
//
// options: { eol, trailing, bom }。eol に関数を渡すと行ごとに改行を変えられる
// （添字 0 がヘッダー）。
export function workingCopy(items, options = {}) {
  const body = joinLines([HEADER.working, ...items.map((item) => dataLine(item, true))], options);
  return (options.bom ? BOM : "") + body;
}

// publishedFile は公開ファイルの中身を組む。行番号の数え方は workingCopy と同じ。
export function publishedFile(items, options = {}) {
  const body = joinLines([HEADER.published, ...items.map((item) => dataLine(item, false))], options);
  return (options.bom ? BOM : "") + body;
}

// scriptOrder は再生順を組む。items は { source, key, section, phase, node, order, lineId, speaker }。
export function scriptOrder(items, options = {}) {
  const rows = items.map((item, i) =>
    record(
      item.section ?? "L01 Ryan",
      item.phase ?? "intro",
      item.node ?? "Ryan_1_intro",
      item.order ?? String(i + 1),
      item.lineId ?? `line:${String(i + 1).padStart(8, "0")}`,
      item.key ?? keyFor(item.source ?? ""),
      item.speaker ?? "",
      item.condition ?? "",
    ),
  );
  return joinLines([HEADER.order, ...rows], options);
}

// SAMPLE は既定の見本に入れる台詞。3行とも再生順にある。
export const SAMPLE = {
  hello: { source: "Hello?", speaker: "Ryan", order: "1", ja: "もしもし？", he: "שלום" },
  goodbye: { source: "Goodbye.", speaker: "Ryan", order: "2", ja: "" },
  wonderful: { source: "Wonderful!", speaker: "Kobold", order: "3", ja: "すばらしい！" },
};

// SAMPLE_LINES は既定の見本の ja 作業コピーで、各台詞が何行目にあるか。
//
//   1 ヘッダー / 2 空行 / 3 節の見出し / 4 節点の見出し / 5 hello / 6 goodbye（訳が空）
//   7 wonderful / 8 空行
export const SAMPLE_LINES = { hello: 5, goodbye: 6, wonderful: 7 };

// sampleWorkingCopy は既定の見本の ja 作業コピー。options は workingCopy と同じ。
export function sampleWorkingCopy(options = {}) {
  const s = SAMPLE;
  return workingCopy(
    [
      "",
      "# ===== Level 1: Ryan (Sunny) =====",
      "# --- intro: Ryan_1_intro ---",
      { ...s.hello, translation: s.hello.ja },
      { ...s.goodbye, translation: s.goodbye.ja },
      { ...s.wonderful, translation: s.wonderful.ja },
      "",
    ],
    options,
  );
}

// sampleRepo は既定の見本を返す。
//
// ロケールは ja と he の2つ。ja は作業コピーを持つので原文の欄が埋まり、
// 「未翻訳」も判定される。he は公開ファイルだけなので、画面は公開ファイル
// そのものを並べる（入力と出力が同じ）。
//
// options:
//   game          true なら ja の作業コピーをゲーム側に置き、--game で起動する
//   workingCopy   ja の作業コピーの中身を差し替える（文字列か Buffer）
export function sampleRepo(options = {}) {
  const s = SAMPLE;
  const working = options.workingCopy ?? sampleWorkingCopy();
  const root = {
    "data/script_order.csv": scriptOrder([s.hello, s.goodbye, s.wonderful]),
    // publish は訳が空の行を書かないので、goodbye は公開ファイルに無い。
    "Translations/ja/strings.csv": publishedFile([
      "",
      "# --- intro: Ryan_1_intro ---",
      { ...s.hello, translation: s.hello.ja },
      { ...s.wonderful, translation: s.wonderful.ja },
      "",
    ]),
    "Translations/he/strings.csv": publishedFile([
      "",
      "# --- intro: Ryan_1_intro ---",
      { ...s.hello, translation: s.hello.he },
      "",
    ]),
  };
  if (!options.game) {
    root["Translations/_discovered/ja.working.csv"] = working;
    return { root, game: null };
  }
  return {
    root,
    game: {
      "Translations/_discovered/ja.working.csv": working,
      // ゲームに入っている公開ファイル。コミット済みとそろえておくと、
      // 書き出し（published）の「土台の食い違い」に引っかからない。
      "Translations/ja/strings.csv": root["Translations/ja/strings.csv"],
    },
  };
}
