// 作業コピーのバイト列を、レコードの単位で比べる道具。
//
// 保存は、触ったレコードの最終フィールドだけを差し替え、ほかは1バイトも変えない
// （internal/edit の doc.go）。引用符で囲んだ値に改行があるレコードは複数の物理行に
// またがるので、物理行ごとに割って比べる道具（いくつかの spec にある splitLines）では、
// どの物理行がどのレコードかを試験の側で決めることになる。ここではファイル全体の
// バイトで比べる。
//
// 期待値は、見本を組んだときのレコードの字（repo.mjs の record で組んだもの）を、before の
// 中で1か所だけ差し替えて作る。試験の側で CSV を割らない。割り方を試験にも持つと、試す
// 相手と同じ誤りを二重に抱える。行の終端（CRLF や LF）はレコードの字に含めないので、
// 差し替えたあとも before の終端がそのまま残る。

// replaceRecord は before の中の from（レコードの字）を to に差し替えたバイト列を返す。
//
// from がちょうど1か所に無ければ投げる。無いのは見本の組み方の誤りで、2か所以上あると
// どちらを差し替えたかで期待値が変わる。
export function replaceRecord(before, from, to) {
  const was = Buffer.isBuffer(before) ? before : Buffer.from(before, "utf8");
  const needle = Buffer.from(from, "utf8");
  const at = was.indexOf(needle);
  if (at < 0) {
    throw new Error(`見本に次のレコードがありません: ${JSON.stringify(from)}`);
  }
  if (was.indexOf(needle, at + 1) >= 0) {
    throw new Error(`見本に次のレコードが2か所以上あります: ${JSON.stringify(from)}`);
  }
  return Buffer.concat([was.subarray(0, at), Buffer.from(to, "utf8"), was.subarray(at + needle.length)]);
}

// expectSameBytes は actual が expected とバイト単位で同じことを確かめ、違えば投げる。
//
// 投げる文には、最初に違う位置と、その前後の字を入れる。Buffer をそのまま比べると、
// 違いが数千バイトの16進の並びとして出て、どこが違うか読めない。
export function expectSameBytes(actual, expected, label = "ファイル") {
  if (actual.equals(expected)) {
    return;
  }
  let at = 0;
  while (at < actual.length && at < expected.length && actual[at] === expected[at]) {
    at++;
  }
  const around = (buf) => JSON.stringify(buf.subarray(Math.max(0, at - 40), at + 40).toString("utf8"));
  throw new Error(
    `${label}が期待と違います（${at} バイト目から。長さは ${actual.length} と ${expected.length}）\n` +
      `  実際: ${around(actual)}\n` +
      `  期待: ${around(expected)}`,
  );
}
