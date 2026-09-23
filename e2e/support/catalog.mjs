// 画面の文言を目録（internal/web/ui/i18n/<言語>.json）から引く。
//
// 試験に日本語を書き写さないためにある。書き写すと、目録を直したときに試験の側だけが
// 古い文言のまま残り、画面が正しいのに落ちるか、間違っているのに通る。
// 置換の規則は app.js の t() と同じ（{name} の1形式だけ）。
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { root } from "./paths.mjs";

const cache = new Map();

// catalog は目録の messages を返す。
export function catalog(lang = "ja") {
  if (!cache.has(lang)) {
    const path = join(root, "internal", "web", "ui", "i18n", `${lang}.json`);
    cache.set(lang, JSON.parse(readFileSync(path, "utf8")).messages);
  }
  return cache.get(lang);
}

// msg は文言を1つ組む。鍵が無ければ投げる（app.js は鍵をそのまま出すが、試験では
// 綴り違いの鍵で「鍵がそのまま出ている画面」と一致して通ってしまうのを避けたい）。
export function msg(lang, key, params) {
  const text = catalog(lang)[key];
  if (typeof text !== "string") {
    throw new Error(`目録（${lang}）に ${key} がありません`);
  }
  if (!params) {
    return text;
  }
  return text.replace(/\{(\w+)\}/g, (whole, name) =>
    Object.prototype.hasOwnProperty.call(params, name) ? String(params[name]) : whole,
  );
}
