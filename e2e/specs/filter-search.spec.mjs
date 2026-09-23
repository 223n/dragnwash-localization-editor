// 絞り込みと検索を見る。
//
// どちらも 1721 行から「いま直すべき行」へ早く着くための道具で、ブラウザーの中だけで
// 終わる（internal/web の doc.go「絞り込みと検索」）。ここで見るのは次の3つである。
//
//   1. 訳を失わない。未保存の訳がある行・保存できなかった行・いま入力欄が開いている
//      行は、条件に当たらなくても隠さない（app.js の keepAlways）。隠すと、直すべき行と
//      触っている行が画面から消え、翻訳者は消えたことに気づけない。
//   2. 画面が判断を持たない。条件の一覧も名前も数も、待ち受けが返した件数から取る。
//   3. 検索語を外へ出さない。URL にも待ち受けへの要求にも保存域にも残さない。
//
// 見本はカテゴリが複数立つように組んである（下の finderRepo）。行番号は物理行で、
// 保存の要求もこの番号で行を指す。
import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { keyFor, publishedFile, record, scriptOrder, workingCopy } from "../support/repo.mjs";
import {
  editor,
  openApp,
  openEditor,
  rowByLine,
  saveState,
  translationCell,
  typeTranslation,
  waitForSaved,
} from "../support/ui.mjs";

const workingRel = "Translations/_discovered/ja.working.csv";

// 見本の台詞。section / node / order / speaker は作業コピーと再生順の両方に同じものを書く。
const T = {
  hello: { source: "Hello?", speaker: "Ryan", section: "L01 Ryan", node: "Ryan_1_intro", order: "1", ja: "もしもし？" },
  goodbye: { source: "Goodbye.", speaker: "Ryan", section: "L01 Ryan", node: "Ryan_1_intro", order: "2", ja: "" },
  wonderful: {
    source: "Wonderful!",
    speaker: "Kobold",
    section: "L01 Ryan",
    node: "Ryan_1_outro",
    phase: "outro",
    order: "3",
    ja: "すばらしい！",
  },
  soap: {
    source: "Where is the soap?",
    speaker: "Kobold",
    section: "L02 Kobold",
    node: "Kobold_2_intro",
    order: "1",
    ja: "",
  },
  scrub: {
    source: "Scrub harder!",
    speaker: "Mira",
    section: "L02 Kobold",
    node: "Kobold_2_intro",
    order: "2",
    ja: "もっとこすって！",
  },
  // 公開ファイルにあるが再生順に無く、section が UI でない。internal/diff は「台本から消えた行」と見る。
  gone: { source: "Old line.", speaker: "Ryan", section: "L02 Kobold", node: "Kobold_2_intro", order: "9", ja: "きえた行" },
  // 公開ファイルにあるが再生順に無く、section も speaker も UI。「由来を判定できない行」になる。
  ui: { source: "Settings", speaker: "UI", section: "UI", node: "", order: "", ja: "設定" },
};

// he にだけ訳がある台詞。ja の作業コピーには行が無いので、ja の「他のロケールにあって無い行」が
// 1件立つのに、この一覧には1行も出せない（countView.noRowHere）。
const bubbles = {
  source: "Bubbles!",
  speaker: "Mira",
  section: "L02 Kobold",
  node: "Kobold_2_intro",
  order: "3",
  he: "בועות",
};

// 見出し（ファイルのコメント行）。画面はこれを書き換えずに写す。
const H = {
  level1: "# ===== Level 1: Ryan (Sunny) =====",
  intro1: "# --- intro: Ryan_1_intro ---",
  memo: "# memo: check tone",
  outro1: "# --- outro: Ryan_1_outro ---",
  level2: "# ===== Level 2: Kobold =====",
  intro2: "# --- intro: Kobold_2_intro ---",
  note: "# note: Mira joins here",
  ui: "# ===== UI =====",
};

// 列数がヘッダーと合わない行。編集できない行として生のまま出る。
const BROKEN = "Broken <b>raw</b> cell,only two";

// 作業コピーの物理行。1行目がヘッダー。
//
//    3 節 Level 1        4 節点 intro       5 hello        6 goodbye（未翻訳）
//    7 メモ（other）     8 節点 outro       9 wonderful
//   11 節 Level 2       12 節点 intro      13 soap（未翻訳） 14 メモ（other）
//   15 scrub            16 gone（台本から消えた行）          17 編集できない行
//   19 節 UI            20 ui（由来を判定できない行）
const LINE = { hello: 5, goodbye: 6, wonderful: 9, soap: 13, scrub: 15, gone: 16, broken: 17, ui: 20 };
const ALL_ROWS = ["5", "6", "9", "13", "15", "16", "17", "20"];

function finderRepo() {
  const withJa = (item) => ({ ...item, translation: item.ja });
  return {
    root: {
      "data/script_order.csv": scriptOrder([T.hello, T.goodbye, T.wonderful, T.soap, T.scrub, bubbles]),
      // publish は訳が空の行を書かないので、goodbye と soap は公開ファイルに無い。
      "Translations/ja/strings.csv": publishedFile([
        "",
        H.intro1,
        withJa(T.hello),
        withJa(T.wonderful),
        withJa(T.scrub),
        withJa(T.gone),
        withJa(T.ui),
        "",
      ]),
      "Translations/he/strings.csv": publishedFile([
        "",
        { ...T.hello, translation: "שלום" },
        { ...bubbles, translation: bubbles.he },
        "",
      ]),
      [workingRel]: workingCopy([
        "",
        H.level1,
        H.intro1,
        withJa(T.hello),
        withJa(T.goodbye),
        H.memo,
        H.outro1,
        withJa(T.wonderful),
        "",
        H.level2,
        H.intro2,
        withJa(T.soap),
        H.note,
        withJa(T.scrub),
        withJa(T.gone),
        BROKEN,
        "",
        H.ui,
        withJa(T.ui),
        "",
      ]),
    },
    game: null,
  };
}

test.use({ repo: finderRepo() });

// 検索の待ち（app.js の searchDelay）と自動保存の待ち（/api/bootstrap の autosaveDelayMs）。
// 時計を進めるときに、境目より少し先まで進めるための値。
const searchDelay = 120;
const autosaveDelay = 1500;

// cat はカテゴリの表示名。待ち受けは目録の category.<識別子> をそのまま返す。
function cat(id) {
  return msg("ja", `category.${id}`);
}

function escapeRegExp(text) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// chip は名前が label の条件1つ。名前はクラスの無い span に入る（app.js の filterChip）。
function chip(page, label) {
  return page.locator("#filters label.chip").filter({
    has: page.locator("span:not([class])", { hasText: new RegExp(`^${escapeRegExp(label)}$`) }),
  });
}

function chipBox(page, label) {
  return chip(page, label).locator('input[type="checkbox"]');
}

function searchBox(page) {
  return page.locator("#search");
}

// visibleRows はいま出ている行の番号。隠れた行は hidden 属性を持つ（app.js の setHidden）。
function visibleRows(page) {
  return page.locator("#list .row:not([hidden]) > .cell.num");
}

function visibleHeadings(page) {
  return page.locator("#list .heading:not([hidden])");
}

function shown(page) {
  return page.locator("#shown");
}

function shownText(count) {
  return msg("ja", "ui.shown", { count });
}

// lineText は作業コピーの1行を、訳を translation にして組む（改行は付けない）。
function lineText(item, translation) {
  return record(keyFor(item.source), item.section, item.node, item.order, item.speaker, item.source, translation);
}

// splitLines は改行を残したまま物理行に分ける。
function splitLines(buf) {
  const out = [];
  let start = 0;
  for (let i = 0; i < buf.length; i++) {
    if (buf[i] === 0x0a) {
      out.push(buf.subarray(start, i + 1));
      start = i + 1;
    }
  }
  if (start < buf.length) {
    out.push(buf.subarray(start));
  }
  return out;
}

// expectOnlyLines は、changed に挙げた行だけが書き換わり、ほかの行は1バイトも
// 変わっていないことを確かめる。changed は { 行番号: 改行を除いた行 }。
async function expectOnlyLines(server, before, changed) {
  const after = await server.readRoot(workingRel);
  const beforeLines = splitLines(before);
  const afterLines = splitLines(after);
  expect(afterLines).toHaveLength(beforeLines.length);
  for (let i = 0; i < beforeLines.length; i++) {
    const text = changed[i + 1];
    if (text === undefined) {
      expect(afterLines[i].equals(beforeLines[i]), `${i + 1}行目が変わった`).toBe(true);
    } else {
      expect(afterLines[i].toString("utf8"), `${i + 1}行目`).toBe(`${text}\n`);
    }
  }
}

// openPaused は時計を差し替えてから画面を開き、開き終えたところで時計を止める。
//
// 検索の待ち（120ms）と自動保存（1.5 秒）と送り直しの間隔を、試験の側で進める。
// 実時間に任せると、遅い機械では「待ちが切れる前に次の操作をする」という前提が崩れる。
// install は goto より前でないと効かないので、app ではなく page と server を使う。
async function openPaused(page, server) {
  await page.clock.install();
  await openApp(page, server);
  const now = await page.evaluate(() => Date.now());
  await page.clock.pauseAt(now + 1000);
}

// tamperKey は、行 line の保存の要求だけキーを書き換えて待ち受けへ流す。
//
// 待ち受けは行番号とキーが食い違う行を書かずに断る（error.row_moved）。1行も書けないので
// 422 が返り、画面はその行を「保存できなかった行」にする。応答は待ち受けが作ったもので、
// 試験がこしらえた本文ではない。
function tamperKey(line) {
  return async (route) => {
    const body = route.request().postDataJSON();
    for (const edit of body.edits) {
      if (edit.line === line) {
        edit.key = "0000000000000000";
      }
    }
    await route.continue({ postData: JSON.stringify(body) });
  };
}

// 条件の並びと名前と数は待ち受けが決める（countView）。画面で組み直すと、internal/diff が
// 避けている誤検出を画面が作り直すことになる。数の書き方は3通りあり、「（0 行）」と
// 書いてはいけない場合（未判定、この一覧には出せない）を取り違えると、残っている作業が
// 「片付いた」と読まれる。添え書き（title）は件数の欄の同じ行で、押す手の下で両方の数を
// 読めるようにするためにある。
test("条件のチップは待ち受けの件数の並びのまま、重さの名前・行数・件数の欄と同じ添え書きを持つ", async ({
  page,
  server,
}) => {
  const linesResponse = page.waitForResponse((res) => new URL(res.url()).pathname === "/api/lines");
  await openApp(page, server);
  const counts = (await (await linesResponse).json()).counts;

  // 見本が3通りの数の書き方を全部踏むことを先に確かめる。踏んでいなければ見本の作りが悪い。
  expect(counts.find((c) => c.category === "untranslated")).toMatchObject({ judged: true, rows: 2, noRowHere: false });
  expect(counts.find((c) => c.category === "carryover")).toMatchObject({ judged: false });
  expect(counts.find((c) => c.category === "locale_gap")).toMatchObject({ judged: true, count: 1, noRowHere: true });

  // カテゴリのぶんと、画面の状態の2つ（未保存・保存できない）。
  const chips = page.locator("#filters label.chip");
  await expect(chips).toHaveCount(counts.length + 2);
  await expect(chips.locator("span:not([class])")).toHaveText([
    ...counts.map((c) => c.label),
    msg("ja", "ui.filter_pending"),
    msg("ja", "ui.filter_failed"),
  ]);
  // 重さの名前も添える。色だけで重さを伝えない。
  await expect(chips.locator(".chip-status")).toHaveText([
    ...counts.map((c) => c.statusLabel),
    msg("ja", "ui.filter_state"),
    msg("ja", "ui.filter_state"),
  ]);

  // 並びは待ち受けの並び（要作業 → 要確認 → 参考）のまま。
  const rank = { todo: 0, review: 1, info: 2 };
  const ranks = counts.map((c) => rank[c.status]);
  expect(ranks).toEqual([...ranks].sort((a, b) => a - b));
  expect(new Set(counts.map((c) => c.status))).toEqual(new Set(["todo", "review", "info"]));
  for (const [i, c] of counts.entries()) {
    await expect(chips.nth(i)).toHaveClass(new RegExp(`(^|\\s)${c.status}(\\s|$)`));
  }

  // 数は待ち受けが数えた行数。判定していない・この一覧に出せないカテゴリには数を出さない。
  const rowsText = (c) => {
    if (!c.judged) {
      return msg("ja", "ui.chip_not_judged");
    }
    if (c.noRowHere) {
      return msg("ja", "ui.chip_no_row_here");
    }
    return msg("ja", "ui.chip_rows", { count: c.rows });
  };
  await expect(chips.locator(".chip-rows")).toHaveText(counts.map(rowsText));
  await expect(chip(page, cat("untranslated")).locator(".chip-rows")).toHaveText(
    msg("ja", "ui.chip_rows", { count: 2 }),
  );

  // 添え書きは件数の欄の同じ行と同じ文。
  const countTexts = await page.locator("#counts > li > span:last-child").allTextContents();
  expect(countTexts).toHaveLength(counts.length);
  for (const [i, text] of countTexts.entries()) {
    await expect(chips.nth(i)).toHaveAttribute("title", text);
  }
  const carryover = counts.find((c) => c.category === "carryover");
  await expect(chip(page, cat("carryover"))).toHaveAttribute(
    "title",
    msg("ja", "ui.not_judged", { reason: carryover.reason }),
  );
  await expect(chip(page, cat("locale_gap"))).toHaveAttribute(
    "title",
    msg("ja", "ui.count_and_rows", { count: 1, rows: 0 }),
  );
  await expect(chip(page, cat("untranslated"))).toHaveAttribute("title", msg("ja", "ui.count_value", { count: 2 }));

  // 画面の状態の2つは待ち受けが数えないので、数も添え書きも持たない。
  for (const [i, status] of [
    [counts.length, "pending"],
    [counts.length + 1, "failed"],
  ]) {
    const stateChip = chips.nth(i);
    await expect(stateChip).toHaveClass(new RegExp(`(^|\\s)${status}(\\s|$)`));
    await expect(stateChip.locator(".chip-rows")).toHaveCount(0);
    expect(await stateChip.getAttribute("title")).toBeNull();
  }
  // 何も選んでいなければ全部出る。
  await expect(chips.locator('input[type="checkbox"]:checked')).toHaveCount(0);
  await expect(visibleRows(page)).toHaveText(ALL_ROWS);
});

// 1行は複数のカテゴリに当たる。1つずつしか選べないと同じ画面を2周することになるので、
// 条件は論理和にしてある（doc.go「条件は複数選べる」）。論理積になっていると、2つ目を
// 選んだ瞬間に行が減り、選んだ条件の行が消えたように見える。
test("条件は論理和で、選んだどれかに当たる行だけを出す", async ({ app }) => {
  await expect(visibleRows(app)).toHaveText(ALL_ROWS);
  await expect(shown(app)).toHaveText(shownText(8));

  await chipBox(app, cat("untranslated")).check();
  await expect(visibleRows(app)).toHaveText(["6", "13"]);
  await expect(shown(app)).toHaveText(shownText(2));

  await chipBox(app, cat("vanished")).check();
  await expect(visibleRows(app)).toHaveText(["6", "13", "16"]);

  await chipBox(app, cat("unknown_origin")).check();
  await expect(visibleRows(app)).toHaveText(["6", "13", "16", "20"]);
  await expect(shown(app)).toHaveText(shownText(4));

  await chipBox(app, cat("untranslated")).uncheck();
  await expect(visibleRows(app)).toHaveText(["16", "20"]);

  await chipBox(app, cat("vanished")).uncheck();
  await chipBox(app, cat("unknown_origin")).uncheck();
  await expect(visibleRows(app)).toHaveText(ALL_ROWS);
  await expect(shown(app)).toHaveText(shownText(8));
});

// 見出しは、その下に出ている行があるときだけ出す。前の節・節点に属していた見出しを
// 持ち越すと、下の行が1つも出ていないメモや節点名が、別の節の行の見出しとして出続け、
// 翻訳者はいま直している行がどこの台詞かを取り違える（applyView の注記）。
test("見出しは下に出ている行のあるものだけを出し、節や節点が変われば前の見出しを持ち越さない", async ({ app }) => {
  // 節点が変わったので、その前の節点の末尾にあるメモ（7行目）は出さない。
  await searchBox(app).fill("wonderful");
  await expect(visibleRows(app)).toHaveText(["9"]);
  await expect(visibleHeadings(app)).toHaveText([H.level1, H.outro1]);

  // 同じ節点の中で、行より上にあるメモは出す。
  await searchBox(app).fill("mira");
  await expect(visibleRows(app)).toHaveText(["15"]);
  await expect(visibleHeadings(app)).toHaveText([H.level2, H.intro2, H.note]);

  // 節が変わったので、前の節の節点（12行目）とメモ（14行目）を持ち越さない。
  await searchBox(app).fill("settings");
  await expect(visibleRows(app)).toHaveText(["20"]);
  await expect(visibleHeadings(app)).toHaveText([H.ui]);

  // 条件で絞ったときも同じ決め方をする。
  await searchBox(app).fill("");
  await chipBox(app, cat("untranslated")).check();
  await expect(visibleRows(app)).toHaveText(["6", "13"]);
  await expect(visibleHeadings(app)).toHaveText([H.level1, H.intro1, H.level2, H.intro2]);
});

// 検索は、打った字を speaker・原文・訳・キーのどれかに含む行を出す（README の表）。
// 大文字小文字を区別すると、原文の "Soap" を "soap" で探せない。訳は打つたびに変わるので、
// 保存した訳でも当たらなければならない（setShownText が小文字の控えを更新する）。
// 編集できない行は生の行で当てる。直せない行を探せないと、どこが壊れているかも追えない。
test("検索は speaker・原文・訳・キー・編集できない行の中身に、大文字小文字を区別せず当たる", async ({ app }) => {
  const cases = [
    ["MIRA", ["15"]], // speaker
    ["SOAP", ["13"]], // 原文
    ["こすって", ["15"]], // 訳
    [keyFor(T.wonderful.source).toUpperCase(), ["9"]], // キー
    ["<B>RAW", ["17"]], // 編集できない行の生のテキスト
  ];
  for (const [term, rows] of cases) {
    await searchBox(app).fill(term);
    await expect(visibleRows(app), term).toHaveText(rows);
  }
  await searchBox(app).fill("");
  await expect(visibleRows(app)).toHaveText(ALL_ROWS);

  // 画面で保存した訳にも当たる。
  await typeTranslation(app, LINE.hello, "Echo ハロー");
  await editor(app).press("Escape");
  await waitForSaved(app);
  await searchBox(app).fill("ECHO");
  await expect(visibleRows(app)).toHaveText([String(LINE.hello)]);
});

// 打鍵ごとに当て直すと、1721 行の描き直しが打つ手に追いつかない。だから打ち終わりから
// 120ms 待ってから当てる（searchDelay）。待ちは打つたびに引き直されるので、打っている
// 途中の字で一覧が跳ねない。
test("検索は打ち終わりから 120ms 待ってから当て、打つたびに待ちを引き直す", async ({ page, server }) => {
  await openPaused(page, server);
  const box = searchBox(page);

  await box.fill("s");
  await page.clock.runFor(100);
  await box.fill("sc");
  await page.clock.runFor(100);
  // 最初の字から 200ms たったが、最後の字からは 100ms。まだ当てない。
  await expect(visibleRows(page)).toHaveText(ALL_ROWS);
  await expect(shown(page)).toHaveText(shownText(8));

  await page.clock.runFor(searchDelay - 100 + 10);
  await expect(visibleRows(page)).toHaveText([String(LINE.scrub)]);
  await expect(shown(page)).toHaveText(shownText(1));
});

// 変換中の読み（「こす」など）で当てると、1行も出ない画面になって打つ手が止まる。
// ja / ko / zh のためにこの入力方式を選んでいるので、変換が終わるまで当てない。
test("検索は変換中は当てず、確定してから当てる", async ({ page, server }) => {
  await openPaused(page, server);
  const box = searchBox(page);

  await box.focus();
  await box.dispatchEvent("compositionstart");
  await box.fill("こすって");
  await page.clock.runFor(1000);
  await expect(visibleRows(page)).toHaveText(ALL_ROWS);

  await box.dispatchEvent("compositionend");
  await expect(visibleRows(page)).toHaveText(ALL_ROWS);
  await page.clock.runFor(searchDelay + 10);
  await expect(visibleRows(page)).toHaveText([String(LINE.scrub)]);
});

// 真っ白な一覧と隅の「表示中 0 行」だけでは、壊れたのか条件に当たっていないのかが
// 読み取れない。だから一覧の場所で言う。検索語のせいで0行のときに「条件を外すと全部出ます」
// と言うと嘘になる（外しても0行のまま）ので、検索の欄に字があればそちらを先に言う。
test("1行も出ないときは一覧の場所でそう言い、検索の欄に字があれば検索を空にするよう言う", async ({ app }) => {
  const empty = app.locator("#empty");
  await expect(empty).toBeEmpty();

  // この一覧には出せないカテゴリ。選ぶと0行になる。
  await chipBox(app, cat("locale_gap")).check();
  await expect(visibleRows(app)).toHaveCount(0);
  await expect(visibleHeadings(app)).toHaveCount(0);
  await expect(shown(app)).toHaveText(shownText(0));
  await expect(empty).toHaveText(msg("ja", "ui.no_rows"));

  await searchBox(app).fill("zzzz");
  await expect(empty).toHaveText(msg("ja", "ui.no_rows_search"));

  // 条件を外しても検索語で0行のまま。言うのは検索のほう。
  await chipBox(app, cat("locale_gap")).uncheck();
  await expect(visibleRows(app)).toHaveCount(0);
  await expect(empty).toHaveText(msg("ja", "ui.no_rows_search"));

  await searchBox(app).fill("");
  await expect(visibleRows(app)).toHaveText(ALL_ROWS);
  await expect(empty).toBeEmpty();
});

// 「全部見たい」を1手でできるようにする（README「条件を外す」）。条件だけが外れて検索語が
// 残ると、全部出ると思って押したのに一覧が絞られたままになる。
test("「条件を外す」で、条件も検索語もまとめて外れて全部の行が出る", async ({ app }) => {
  await chipBox(app, cat("untranslated")).check();
  await chipBox(app, cat("vanished")).check();
  await searchBox(app).fill("ryan");
  await expect(visibleRows(app)).toHaveText(["6", "16"]);

  await app.locator("#filter-clear").click();
  await expect(searchBox(app)).toHaveValue("");
  await expect(app.locator('#filters input[type="checkbox"]:checked')).toHaveCount(0);
  await expect(visibleRows(app)).toHaveText(ALL_ROWS);
  await expect(shown(app)).toHaveText(shownText(8));
  await expect(app.locator("#empty")).toBeEmpty();
});

// 訳を失わないという約束は、絞り込みより重い（keepAlways）。保存が届かず未保存のまま
// 抱えている行を条件で隠すと、翻訳者はその訳がまだファイルに無いことに気づけない。
// 隠さずに出し続け、つながり直したら送り直しでファイルに入ることまで見る。
test("未保存の訳がある行は、条件にも検索にも当たらなくても隠さず、つながり直すとファイルに入る", async ({
  page,
  server,
}) => {
  let blocked = true;
  await page.route("**/api/rows", (route) => (blocked ? route.abort() : route.continue()));
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);
  const typed = "もしもし、聞こえますか？";
  const row = rowByLine(page, LINE.hello);

  // 5行目は訳が入っていてバッジが無い。どの条件にも当たらない行である。
  await typeTranslation(page, LINE.hello, typed);
  await editor(page).press("Escape");
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
  await expect(row).toHaveClass(/(^|\s)unsaved(\s|$)/);

  await chipBox(page, cat("untranslated")).check();
  await expect(visibleRows(page)).toHaveText(["5", "6", "13"]);
  await expect(shown(page)).toHaveText(shownText(3));

  await searchBox(page).fill("soap");
  await page.clock.runFor(searchDelay + 10);
  await expect(visibleRows(page)).toHaveText(["5", "13"]);
  await expect(translationCell(page, LINE.hello)).toHaveText(typed);
  // ここまでファイルは1バイトも変わっていない。訳は画面の中にしか無い。
  expect((await server.readRoot(workingRel)).equals(before)).toBe(true);

  // つながり直したら、送り直しの時計でファイルに入る。
  blocked = false;
  await expect
    .poll(
      async () => {
        await page.clock.runFor(1000);
        return (await server.readRootText(workingRel)).includes(`,${typed}\n`);
      },
      { timeout: 20_000 },
    )
    .toBe(true);
  await waitForSaved(page);
  await expectOnlyLines(server, before, { [LINE.hello]: lineText(T.hello, typed) });
});

// 保存できなかった行も隠さない。直すべき行がまさにそこなので、条件で消えると、打った訳が
// ファイルに入っていないまま画面からも消える。打った訳は欄に出し続ける（shownValue）。
test("保存できなかった行は、条件にも検索にも当たらなくても隠さず、打った訳を出し続ける", async ({ app, server }) => {
  await app.route("**/api/rows", tamperKey(LINE.hello));
  const before = await server.readRoot(workingRel);
  const typed = "もしもし、聞こえますか？";
  const row = rowByLine(app, LINE.hello);

  await typeTranslation(app, LINE.hello, typed);
  await editor(app).press("Escape");
  await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));
  await expect(row).toHaveClass(/(^|\s)save-failed(\s|$)/);

  await chipBox(app, cat("untranslated")).check();
  await expect(visibleRows(app)).toHaveText(["5", "6", "13"]);
  await searchBox(app).fill("soap");
  await expect(visibleRows(app)).toHaveText(["5", "13"]);

  await expect(translationCell(app, LINE.hello)).toHaveText(typed);
  await expect(row.locator(".row-note")).toHaveText(
    msg("ja", "ui.row_error", { reason: msg("ja", "error.row_moved") }),
  );
  // 待ち受けは食い違う行を書かない。
  expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
});

// 検索欄に打ってから 120ms 以内に行の訳欄を押すと、入力欄が開いた直後に当て直しが走る。
// 以前はそこでまだ1字も打っていない行を閉じて隠し、以後打った字はどこにも入らないのに
// 「保存済み」のままだった（keepAlways の注記、実際に起きた）。触っている行は隠さない。
test("検索の待ちのあいだに開いた入力欄は、当て直しで隠れず、打った字がファイルに入る", async ({ page, server }) => {
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);
  const typed = "もしもし、聞こえますか？";

  // 検索語を打ち、待ちが切れる前に、検索に当たらない5行目を押す。
  await searchBox(page).fill("soap");
  await translationCell(page, LINE.hello).click();
  await expect(editor(page)).toBeFocused();
  await page.clock.runFor(searchDelay + 80);

  // 当て直しは走ったが、入力欄の開いている5行目は出たまま、焦点も入力欄のまま。
  await expect(visibleRows(page)).toHaveText(["5", "13"]);
  await expect(shown(page)).toHaveText(shownText(2));
  await expect(rowByLine(page, LINE.hello).locator("textarea.editor")).toHaveCount(1);
  await expect(editor(page)).toBeVisible();
  await expect(editor(page)).toBeFocused();

  // 打鍵が入力欄へ届く（fill は焦点を移し直すので、ここではキーボードで打つ）。
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type(typed);
  await expect(editor(page)).toHaveValue(typed);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));

  // Enter で確定すると、隠れている行を飛ばして、出ている次の行（13行目）が開く。
  await page.keyboard.press("Enter");
  await expect(rowByLine(page, LINE.soap).locator("textarea.editor")).toHaveCount(1);
  await expect
    .poll(async () => (await server.readRootText(workingRel)).includes(`,${typed}\n`), { timeout: 10_000 })
    .toBe(true);
  await waitForSaved(page);
  await expectOnlyLines(server, before, { [LINE.hello]: lineText(T.hello, typed) });
});

// 保存のたびに当て直すと、いま訳し終えた行が「未翻訳」の条件から外れて目の前で消える
// （applyView の注記）。条件は翻訳者が決めた時点の写しのままにし、チップの数だけを
// 待ち受けが数え直した数へ書き換える。選んでいるチップも外れない。
test("保存のたびには当て直さず、訳し終えた行は一覧に残り、チップの数だけが変わる", async ({ app, server }) => {
  const before = await server.readRoot(workingRel);
  const typed = "さようなら。";
  const untranslated = chip(app, cat("untranslated"));

  await chipBox(app, cat("untranslated")).check();
  await expect(visibleRows(app)).toHaveText(["6", "13"]);

  await typeTranslation(app, LINE.goodbye, typed);
  await editor(app).press("Enter");
  // 隠れている9行目を飛ばして、出ている次の行（13行目）が開く。
  await expect(rowByLine(app, LINE.soap).locator("textarea.editor")).toHaveCount(1);
  await expect
    .poll(async () => (await server.readRootText(workingRel)).includes(`,${typed}\n`), { timeout: 10_000 })
    .toBe(true);
  await waitForSaved(app);

  // 保存の応答で6行目の「未翻訳」は外れたが、一覧は組み直さない。
  await expect(rowByLine(app, LINE.goodbye).locator(".badge")).toHaveCount(0);
  await expect(visibleRows(app)).toHaveText(["6", "13"]);
  await expect(shown(app)).toHaveText(shownText(2));
  await expect(chipBox(app, cat("untranslated"))).toBeChecked();
  // チップの数と添え書きは、待ち受けが数え直したものに変わる。
  await expect(untranslated.locator(".chip-rows")).toHaveText(msg("ja", "ui.chip_rows", { count: 1 }));
  await expect(untranslated).toHaveAttribute("title", msg("ja", "ui.count_value", { count: 1 }));
  await expectOnlyLines(server, before, { [LINE.goodbye]: lineText(T.goodbye, typed) });
});

// 入力欄を閉じたあと、その行がもう条件に当たらないのに残っていると、「表示中 N 行」も
// その行を数えたままになり、翻訳者が次に条件を触るまで食い違う（reviewClosed の注記）。
// マウスで別の行へ移る道も、Escape で閉じる道も、閉じたところで照らし直す。
test("閉じた行がもう条件に当たらなければ、閉じたところで隠れる", async ({ page, server }) => {
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);
  await chipBox(page, cat("untranslated")).check();
  await expect(visibleRows(page)).toHaveText(["6", "13"]);

  // 6行目に訳を入れ、自動保存を待つ。入力欄は開いたまま。
  await typeTranslation(page, LINE.goodbye, "さようなら。");
  await page.clock.runFor(autosaveDelay + 100);
  await waitForSaved(page);
  await expect(rowByLine(page, LINE.goodbye).locator(".badge")).toHaveCount(0);
  await expect(visibleRows(page)).toHaveText(["6", "13"]);

  // マウスで13行目へ移ると、閉じた6行目はもう「未翻訳」でないので隠れる。
  await translationCell(page, LINE.soap).click();
  await expect(rowByLine(page, LINE.soap).locator("textarea.editor")).toHaveCount(1);
  await expect(editor(page)).toBeFocused();
  await expect(visibleRows(page)).toHaveText(["13"]);
  await expect(shown(page)).toHaveText(shownText(1));

  // 13行目も訳して保存し、Escape で閉じると隠れて、一覧は0行になる。
  await editor(page).fill("石けんはどこ？");
  await page.clock.runFor(autosaveDelay + 100);
  await waitForSaved(page);
  await editor(page).press("Escape");
  await expect(visibleRows(page)).toHaveCount(0);
  await expect(shown(page)).toHaveText(shownText(0));
  await expect(page.locator("#empty")).toHaveText(msg("ja", "ui.no_rows"));
  // 隠れたのは画面の上だけで、訳した2行はファイルに入っている。
  await expectOnlyLines(server, before, {
    [LINE.goodbye]: lineText(T.goodbye, "さようなら。"),
    [LINE.soap]: lineText(T.soap, "石けんはどこ？"),
  });
});

// Enter の行送りでは、次の行を開いてから閉じた行を照らし直す。逆の順だと、照らし直しの
// 時点で「いま開いている行」が空なので、これから開く行が条件に当たらなければその場で
// 隠れ、入力欄が隠れた行へ差し込まれる。焦点は入っているのに欄は見えず、打った字は
// どこにも見えないまま入る（reviewClosed の注記に実測がある）。
test("Enter の行送りでは次の行を開いてから照らし直し、入力欄が隠れた行に入らない", async ({ page, server }) => {
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);
  await chipBox(page, cat("untranslated")).check();
  await expect(visibleRows(page)).toHaveText(["6", "13"]);

  // 6行目と13行目を訳す。どちらも閉じる時点では未保存なので、一覧に残る。
  await typeTranslation(page, LINE.goodbye, "さようなら。");
  await editor(page).press("Enter");
  await expect(rowByLine(page, LINE.soap).locator("textarea.editor")).toHaveCount(1);
  await editor(page).fill("石けんはどこ？");
  await editor(page).press("Escape");
  await expect
    .poll(
      async () => {
        // 6行目の保存が返ってから、13行目は自動保存の時計で送られる。
        await page.clock.runFor(autosaveDelay + 100);
        const text = await server.readRootText(workingRel);
        return text.includes(",さようなら。\n") && text.includes(",石けんはどこ？\n");
      },
      { timeout: 20_000 },
    )
    .toBe(true);
  await waitForSaved(page);
  // どちらももう「未翻訳」ではないが、保存では当て直さないので出ている。
  await expect(visibleRows(page)).toHaveText(["6", "13"]);

  // 6行目を開いて Enter。次の行（13行目）は条件に当たらない行である。
  await openEditor(page, LINE.goodbye);
  await editor(page).press("Enter");

  // 開いた13行目は出ていて、入力欄は見えて焦点もある。閉じた6行目は隠れる。
  await expect(visibleRows(page)).toHaveText(["13"]);
  await expect(rowByLine(page, LINE.soap).locator("textarea.editor")).toHaveCount(1);
  await expect(editor(page)).toBeVisible();
  await expect(editor(page)).toBeFocused();
  expect((await editor(page).boundingBox()).height).toBeGreaterThan(0);
  await expect(page.locator("#empty")).toBeEmpty();
  await expect(shown(page)).toHaveText(shownText(1));

  // 打った字はその行に入り、ファイルへ届く。
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type("石けんはどこですか？");
  await page.keyboard.press("Escape");
  await expect
    .poll(async () => (await server.readRootText(workingRel)).includes(",石けんはどこですか？\n"), {
      timeout: 10_000,
    })
    .toBe(true);
  await waitForSaved(page);
  await expectOnlyLines(server, before, {
    [LINE.goodbye]: lineText(T.goodbye, "さようなら。"),
    [LINE.soap]: lineText(T.soap, "石けんはどこですか？"),
  });
});

// 「未保存」のチップは、待ち受けが知らない画面の状態で選ぶ条件である（buildFilters）。
// 送れずに抱えている訳を、1721 行の中から拾い出せないと、どれを確かめればよいか分からない。
test("「未保存」のチップを選ぶと、まだファイルに入っていない行だけが並ぶ", async ({ page, server }) => {
  await page.route("**/api/rows", (route) => route.abort());
  await openPaused(page, server);
  const before = await server.readRoot(workingRel);

  await typeTranslation(page, LINE.wonderful, "すごい！");
  await editor(page).press("Escape");
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

  await chipBox(page, msg("ja", "ui.filter_pending")).check();
  await expect(visibleRows(page)).toHaveText([String(LINE.wonderful)]);
  await expect(visibleHeadings(page)).toHaveText([H.level1, H.outro1]);
  await expect(shown(page)).toHaveText(shownText(1));
  await expect(translationCell(page, LINE.wonderful)).toHaveText("すごい！");
  expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
});

// 「保存できない」のチップも同じ。保存できなかった行は自動保存の対象から外れているので、
// 翻訳者が拾い出して直さない限り、その訳はファイルに入らない。
test("「保存できない」のチップを選ぶと、保存できなかった行だけが並ぶ", async ({ app, server }) => {
  await app.route("**/api/rows", tamperKey(LINE.wonderful));
  const before = await server.readRoot(workingRel);

  await typeTranslation(app, LINE.wonderful, "すごい！");
  await editor(app).press("Escape");
  await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));

  await chipBox(app, msg("ja", "ui.filter_failed")).check();
  await expect(visibleRows(app)).toHaveText([String(LINE.wonderful)]);
  await expect(shown(app)).toHaveText(shownText(1));
  await expect(translationCell(app, LINE.wonderful)).toHaveText("すごい！");
  expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
});

// 読み直しは同じロケールを見続けているので、条件と検索語を外さない。外すと、絞って
// 作業している途中で読み直すたびに全行へ戻され、作業の位置を失う。
test("読み直しでは条件と検索語を外さない", async ({ app }) => {
  await chipBox(app, cat("untranslated")).check();
  await searchBox(app).fill("soap");
  await expect(visibleRows(app)).toHaveText(["13"]);

  // 読み直しで一覧とチップが描き直されたことを、印の消えたことで見分ける。
  await app.evaluate(() => {
    document.querySelectorAll("#list > *, #filters > *").forEach((node) => node.setAttribute("data-before", ""));
  });
  await app.locator("#reload").click();
  await expect(app.locator("#list [data-before], #filters [data-before]")).toHaveCount(0);
  await expect(visibleRows(app)).not.toHaveCount(0);

  await expect(chipBox(app, cat("untranslated"))).toBeChecked();
  await expect(searchBox(app)).toHaveValue("soap");
  await expect(visibleRows(app)).toHaveText(["13"]);
  await expect(shown(app)).toHaveText(shownText(1));
});

// 条件はそのロケールを見ながら決めたものである。持ち越すと、ja で打った検索語のまま
// ko へ移ったときに、行数は出ているのに一覧が空になる（doc.go「1行も出ないときは、そう言う」）。
test("ロケールを切り替えて読めたら、条件と検索語を外す", async ({ app }) => {
  await chipBox(app, cat("untranslated")).check();
  await searchBox(app).fill("soap");
  await expect(visibleRows(app)).toHaveText(["13"]);

  await app.locator("#locale").selectOption("he");
  await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 2 }));
  await expect(searchBox(app)).toHaveValue("");
  await expect(app.locator('#filters input[type="checkbox"]:checked')).toHaveCount(0);
  await expect(visibleRows(app)).toHaveText(["3", "4"]);
  await expect(shown(app)).toHaveText(shownText(2));
  // he には作業コピーが無いので、未翻訳は判定していない。チップは he の件数から組み直されている。
  await expect(chip(app, cat("untranslated")).locator(".chip-rows")).toHaveText(msg("ja", "ui.chip_not_judged"));
});

// 外すのは読めたときだけにする。切り替えの手前で外していたころは、読み込みに失敗すると
// 条件と検索欄だけが空になり、チップと一覧は前のロケールのまま残った（load の注記）。
// 失敗したときは何も変わっていないのが正しい。
test("ロケールの読み込みに失敗したときは、条件も検索語も一覧も前のまま", async ({ app }) => {
  await app.route(
    (url) => url.pathname === "/api/lines" && url.searchParams.get("locale") === "he",
    (route) => route.fulfill({ status: 500, contentType: "application/json", body: "{}" }),
  );
  await chipBox(app, cat("untranslated")).check();
  await searchBox(app).fill("soap");
  await expect(visibleRows(app)).toHaveText(["13"]);

  await app.locator("#locale").selectOption("he");
  await expect(app.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
  await expect(app.locator("#locale")).toHaveValue("ja");
  await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 8 }));
  await expect(chipBox(app, cat("untranslated"))).toBeChecked();
  await expect(searchBox(app)).toHaveValue("soap");
  await expect(visibleRows(app)).toHaveText(["13"]);
  await expect(shown(app)).toHaveText(shownText(1));
});

// 検索語は原文の断片である。待ち受けに聞けば記録に残りうるし、URL に載せればブラウザーの
// 履歴に残る。localStorage と sessionStorage はディスクに残る（app.js 冒頭の約束）。
// 行はもうブラウザーの中にあるので、検索にも絞り込みにも要求は1つも要らない。
test("検索と絞り込みは要求を1つも出さず、検索語を URL にも記録にも保存域にも残さない", async ({ app, server }) => {
  const term = "zqsecretterm";
  const requests = [];
  app.on("request", (req) => requests.push(req.url()));

  await searchBox(app).pressSequentially(term);
  await expect(app.locator("#empty")).toHaveText(msg("ja", "ui.no_rows_search"));
  await searchBox(app).fill("soap");
  await expect(visibleRows(app)).toHaveText(["13"]);
  await chipBox(app, cat("untranslated")).check();
  await chipBox(app, msg("ja", "ui.filter_pending")).check();
  await chipBox(app, cat("untranslated")).uncheck();
  await app.locator("#filter-clear").click();
  await expect(visibleRows(app)).toHaveText(ALL_ROWS);

  expect(requests.filter((url) => /^https?:/i.test(url))).toEqual([]);
  expect(app.url()).toBe(`${server.origin}/`);
  expect(await app.title()).not.toContain(term);
  const storage = await app.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }));
  expect(storage).toEqual({ local: 0, session: 0 });
  expect(await server.logText()).not.toContain(term);
  expect(server.stdout()).not.toContain(term);
  expect(server.stderr()).not.toContain(term);
});

// 検索の欄も訳の入力欄と同じ扱いにする。綴り検査は入力の中身を外部のサービスへ送りうる
// 経路で、type=search はブラウザーによって前に打った語を候補として残す（index.html の注記）。
// 向きと言語は、he や ar で打つ人のキャレットのためにロケールへ合わせる。
test("検索の欄は、綴り検査・自動補正・自動補完・翻訳を切った素の文字欄である", async ({ app }) => {
  const box = searchBox(app);
  await expect(box).toHaveAttribute("type", "text");
  await expect(box).toHaveAttribute("spellcheck", "false");
  await expect(box).toHaveAttribute("autocorrect", "off");
  await expect(box).toHaveAttribute("autocapitalize", "off");
  await expect(box).toHaveAttribute("autocomplete", "off");
  await expect(box).toHaveAttribute("translate", "no");
  await expect(box).toHaveClass(/(^|\s)notranslate(\s|$)/);
  expect(await box.evaluate((node) => node.spellcheck)).toBe(false);
  await expect(box).toHaveAttribute("aria-label", msg("ja", "ui.search"));
  await expect(box).toHaveAttribute("placeholder", msg("ja", "ui.search_placeholder"));
  await expect(box).toHaveAttribute("dir", "auto");
  await expect(box).toHaveAttribute("lang", "ja");
});

// チップの数は、待ち受けが数え直した「この一覧にあるその条件の行数」である（updateChipRows）。
// 条件を外すボタンがチップを組み直すとき、起動時に読んだ件数（state.data.counts）から組むと、
// 保存で減った数が保存前の数へ戻り、件数の欄（保存の応答で描き直したもの）とも食い違う。
test("保存したあとで条件を外しても、チップの数と添え書きは待ち受けが数え直したまま", async ({ app }) => {
  const untranslated = chip(app, cat("untranslated"));

  await chipBox(app, cat("untranslated")).check();
  await typeTranslation(app, LINE.goodbye, "さようなら。");
  await editor(app).press("Escape");
  await waitForSaved(app);
  await expect(untranslated.locator(".chip-rows")).toHaveText(msg("ja", "ui.chip_rows", { count: 1 }));

  await app.locator("#filter-clear").click();
  await expect(chipBox(app, cat("untranslated"))).not.toBeChecked();
  const countLine = app.locator("#counts > li").filter({ hasText: cat("untranslated") }).locator("span").last();
  await expect(countLine).toHaveText(msg("ja", "ui.count_value", { count: 1 }));
  await expect(untranslated.locator(".chip-rows")).toHaveText(msg("ja", "ui.chip_rows", { count: 1 }), {
    timeout: 2_000,
  });
  await expect(untranslated).toHaveAttribute("title", msg("ja", "ui.count_value", { count: 1 }), { timeout: 2_000 });
});

test.describe("ロケールを省いて起動したとき", () => {
  test.use({ dwloc: { locale: "" } });

  // いま何行出ているかを言うのは「表示中 N 行」だけである（buildFilters の注記）。読むものが
  // 1行も無いとき（ロケールを選ぶ前）は「当たる行がありません」とも言わない（applyView の
  // 注記）。選んだあとも空の選択肢が残っていたころは、空へ戻すと一覧だけが消え、前の
  // ロケールの行が検索と「表示中」の対象に残った。一覧は空なのに「表示中 8 行」と言い、
  // 翻訳者は画面が壊れたのか条件のせいなのかを読み取れなかった。いまは選んだあとに空の
  // 選択肢を外すので、戻す道が無い（app.js の load）。検索と「表示中」は、並んでいる行だけを見る。
  test("ロケールを選んだあとは空の選択肢へ戻せず、検索と「表示中」は並んでいる行だけを見る", async ({
    page,
    server,
  }) => {
    await openPaused(page, server);
    await expect(page.locator('#locale option[value=""]')).toHaveCount(1);
    await page.locator("#locale").selectOption("ja");
    await expect(visibleRows(page)).toHaveText(ALL_ROWS);
    await expect(page.locator('#locale option[value=""]')).toHaveCount(0);

    await searchBox(page).fill("zzzz");
    await page.clock.runFor(searchDelay + 10);
    await expect(page.locator("#empty")).toHaveText(msg("ja", "ui.no_rows_search"));
    await expect(shown(page)).toHaveText(shownText(0));

    await searchBox(page).fill("");
    await page.clock.runFor(searchDelay + 10);
    await expect(shown(page)).toHaveText(shownText(ALL_ROWS.length));
    await expect(page.locator("#empty")).toBeEmpty();
  });

  // 「条件を外す」は選択を外すだけで、チップを組み直さない。組み直していたころは、ロケールを
  // 選ぶ前に押すと、カテゴリの無いまま「未保存」「保存できない」の2つだけが現れた。
  test("ロケールを選ぶ前に「条件を外す」を押しても、条件のチップを作らない", async ({ page, server }) => {
    await openPaused(page, server);
    await expect(page.locator("#filters label.chip")).toHaveCount(0);
    await page.locator("#filter-clear").click();
    await expect(page.locator("#filters label.chip")).toHaveCount(0);

    await page.locator("#locale").selectOption("ja");
    await expect(visibleRows(page)).toHaveText(ALL_ROWS);
    await expect(page.locator("#filters label.chip")).not.toHaveCount(0);
  });
});
