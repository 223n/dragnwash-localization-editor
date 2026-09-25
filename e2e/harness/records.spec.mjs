// 試験の仕組みのうち、作業コピーをレコードの単位で比べる道具（support/records.mjs）を見る。
//
// この道具が黙って通すと、保存が触っていないレコードを書き換えても試験が通る。差し替える
// レコードが見つからないときと2か所あるときに投げること、違いがあれば投げることを見る。
import { expect, test } from "@playwright/test";

import { expectSameBytes, replaceRecord } from "../support/records.mjs";

test("replaceRecord は1か所だけ差し替え、終端と前後のバイトを残す", () => {
  const before = Buffer.from("﻿key,translation\r\na,\"x\ny\",\r\nb,\r\n", "utf8");
  const after = replaceRecord(before, 'a,"x\ny",', 'a,"x\ny",訳');
  expect(after.toString("utf8")).toBe("﻿key,translation\r\na,\"x\ny\",訳\r\nb,\r\n");
  // 文字列を渡しても同じ。
  expect(replaceRecord(before.toString("utf8"), "b,", "b,び").toString("utf8")).toBe(
    "﻿key,translation\r\na,\"x\ny\",\r\nb,び\r\n",
  );
});

test("replaceRecord は、見つからないときと2か所以上あるときに投げる", () => {
  const before = Buffer.from("key,translation\na,\na,\n", "utf8");
  expect(() => replaceRecord(before, "c,", "c,し")).toThrow(/ありません/);
  expect(() => replaceRecord(before, "a,", "a,あ")).toThrow(/2か所以上/);
});

test("expectSameBytes は同じなら通し、違えば最初に違う位置を添えて投げる", () => {
  const same = Buffer.from("key,translation\na,あ\n", "utf8");
  expectSameBytes(same, Buffer.from(same));
  const other = Buffer.from("key,translation\na,い\n", "utf8");
  // 「あ」と「い」は UTF-8 で先頭の2バイトが同じなので、違うのは 18 + 2 バイト目から。
  expect(() => expectSameBytes(other, same, "作業コピー")).toThrow(/作業コピーが期待と違います（20 バイト目から/);
  // 長さだけが違う（片方が前半そのもの）ときも投げる。
  expect(() => expectSameBytes(same.subarray(0, 5), same)).toThrow(/5 バイト目から/);
});
