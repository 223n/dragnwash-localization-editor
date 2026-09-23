// JavaScript の中で「コードがある行」を数えあげる。
//
// v8-to-istanbul は1行を1つの文として数え、空行とコメントだけの行も数に入れる。
// 数え方は「その行を含む範囲が実行されたか」なので、呼ばれた関数の中のコメントは
// 通ったことになり、呼ばれなかった関数の中のコメントは通っていないことになる。
// app.js は約2800行のうち半分近くがコメントで、しかも長い注記の多くは関数の外
// （いつも実行される即時関数の直下）にある。そのままでは、試験を5つ書いただけで
// 行のカバレッジが7割を超えて見え、閾値の根拠にならない。
//
// そこで、空白とコメントのほかに何も無い行を数から外す。字句を1回なぞるだけの
// 簡単な走査で、文字列・テンプレート・正規表現の中の「//」や「/*」をコメントと
// 取り違えないようにしてある。正規表現と割り算の見分けは、直前の字句で決める
// （JavaScript の字句解析で普通に使われる近似）。終わりまでなぞって文字列や
// コメントの途中で止まっていたら、走査の前提が崩れているので投げる。
const regexAfterWord = new Set([
  "return", "typeof", "instanceof", "in", "of", "new", "delete", "void",
  "throw", "case", "do", "else", "yield", "await",
]);

// regexAllowed は、直前の字句のあとに「/」が来たとき正規表現の始まりかを返す。
function regexAllowed(prev) {
  if (prev === "") {
    return true;
  }
  if (/^[A-Za-z_$][\w$]*$/.test(prev)) {
    return regexAfterWord.has(prev);
  }
  if (/^[\d.]+$/.test(prev)) {
    return false;
  }
  return !(prev === ")" || prev === "]" || prev === "}");
}

// codeLines は、コードを1字でも含む行の番号（1始まり）の集合を返す。
export function codeLines(source) {
  const code = new Set();
  const n = source.length;
  let line = 1;
  let i = 0;
  let state = "code";
  // prev は直前の字句（記号なら1字、語ならその語）。正規表現の見分けに使う。
  let prev = "";
  // braces はテンプレートの ${ の中で数えている中括弧の深さ。
  const braces = [];
  let inClass = false;

  while (i < n) {
    const c = source[i];
    const d = source[i + 1];
    if (c === "\n") {
      line++;
      if (state === "line") {
        state = "code";
      }
      i++;
      continue;
    }
    if (state === "block") {
      if (c === "*" && d === "/") {
        state = "code";
        i += 2;
        continue;
      }
      i++;
      continue;
    }
    if (state === "line") {
      i++;
      continue;
    }
    if (c === " " || c === "\t" || c === "\r" || c === "\f" || c === "\v" || c === "﻿") {
      i++;
      continue;
    }

    if (state === "code") {
      if (c === "/" && d === "*") {
        state = "block";
        i += 2;
        continue;
      }
      if (c === "/" && d === "/") {
        state = "line";
        i += 2;
        continue;
      }
      code.add(line);
      if (c === '"' || c === "'") {
        state = c;
        i++;
        continue;
      }
      if (c === "`") {
        state = "tpl";
        i++;
        continue;
      }
      if (c === "/" && regexAllowed(prev)) {
        state = "regex";
        inClass = false;
        i++;
        continue;
      }
      if (/[A-Za-z_$]/.test(c)) {
        let j = i;
        while (j < n && /[\w$]/.test(source[j])) {
          j++;
        }
        prev = source.slice(i, j);
        i = j;
        continue;
      }
      if (/\d/.test(c)) {
        // 数。1.5 や 1e3 や 0x1f の中の字は正規表現の見分けに関わらないので、まとめて読む。
        let j = i;
        while (j < n && /[\w.]/.test(source[j])) {
          j++;
        }
        prev = "0";
        i = j;
        continue;
      }
      if (c === "{" && braces.length > 0) {
        braces[braces.length - 1]++;
      } else if (c === "}" && braces.length > 0) {
        if (braces[braces.length - 1] === 0) {
          // テンプレートの ${ ... } が閉じた。テンプレートの中へ戻る。
          braces.pop();
          state = "tpl";
          i++;
          continue;
        }
        braces[braces.length - 1]--;
      }
      prev = c;
      i++;
      continue;
    }

    // ここから先は文字列・テンプレート・正規表現の中。空白でない字があれば、その行はコード。
    code.add(line);
    if (c === "\\") {
      // 改行をまたぐ継続行でも、次の字（改行）は上の分岐で数える。
      i += d === "\n" ? 1 : 2;
      continue;
    }
    if (state === '"' || state === "'") {
      if (c === state) {
        state = "code";
        prev = "str";
      }
      i++;
      continue;
    }
    if (state === "tpl") {
      if (c === "`") {
        state = "code";
        prev = "str";
        i++;
        continue;
      }
      if (c === "$" && d === "{") {
        braces.push(0);
        state = "code";
        prev = "{";
        i += 2;
        continue;
      }
      i++;
      continue;
    }
    // 正規表現。文字クラスの中の「/」では終わらない。
    if (c === "[") {
      inClass = true;
    } else if (c === "]") {
      inClass = false;
    } else if (c === "/" && !inClass) {
      state = "code";
      prev = "regex";
    }
    i++;
  }

  if (state !== "code" && state !== "line") {
    throw new Error(`コードの行を数えきれませんでした（終わりが ${state} の途中）`);
  }
  return code;
}
