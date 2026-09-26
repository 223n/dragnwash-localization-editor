// 起動してから行が並ぶまで（目録・ロケールの選択・読み直し）を見る。
//
// 画面は文言を1つも持たず、/api/bootstrap の目録で組み上がる（app.js 冒頭の約束）。
// 行は /api/lines で読み、読み直しとロケールの切り替えでも同じ道を通る。
// ここが崩れると、翻訳者は「どの言語の画面で、どのファイルの、どの版を見ているか」を
// 取り違える。
//
// いちばん重いのは、切り替えと読み直しが訳を捨てる操作だということである。
// 未保存の訳があれば必ず尋ね、断られたら何も変えない。読み込みに失敗したときも
// 何も変えない（app.js の load と askDiscard）。
import { readFileSync } from "node:fs";
import { rm } from "node:fs/promises";
import { join } from "node:path";

import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { root } from "../support/paths.mjs";
import { SAMPLE, SAMPLE_LINES, keyFor, publishedFile, sampleRepo } from "../support/repo.mjs";
import {
  dataRows,
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
const heRel = "Translations/he/strings.csv";
const typed = "さようなら。";

// ロケールの欄で選んでから読みにいくまでの待ち（app.js の localeDelay）。変えたら、ここも合わせる。
// 時計を止めた試験では、選んだあとにこれだけ進めないと読みにいかない。
const localeDelay = 400;

// wrongKey はファイルに無いキー。保存の要求をこれに差し替えると、待ち受けは
// 「この行はずれています」で書かずに断る（internal/web の keyMatches）。行ごとの
// 失敗を、待ち受けの本物の応答で作るために使う。
const wrongKey = keyFor("この原文は見本のどこにも無い");

// meta は目録ファイルの lang と dir。catalog.mjs は messages しか返さないので、ここで読む。
function meta(lang) {
  const path = join(root, "internal", "web", "ui", "i18n", `${lang}.json`);
  const json = JSON.parse(readFileSync(path, "utf8"));
  return { lang: json.lang, dir: json.dir };
}

// fileLabel は #file-path に出る1行。
function fileLabel(lang, path) {
  return `${msg(lang, "ui.file")}: ${path}`;
}

// isPath は URL のパスだけで当てる述語。問い合わせ文字列の有無に左右されない。
function isPath(pathname) {
  return (url) => url.pathname === pathname;
}

// watchRequests は、ある経路への要求を数える。
function watchRequests(page, pathname) {
  const seen = [];
  page.on("request", (req) => {
    if (new URL(req.url()).pathname === pathname) {
      seen.push(req);
    }
  });
  return seen;
}

// watchDialogs はダイアログを控え、accept が真なら受け、偽なら断る。
function watchDialogs(page, accept) {
  const seen = [];
  page.on("dialog", async (dialog) => {
    seen.push({ type: dialog.type(), message: dialog.message() });
    if (accept) {
      await dialog.accept();
    } else {
      await dialog.dismiss();
    }
  });
  return seen;
}

// gate は route の手を止めておくための栓。release を呼ぶまで開かない。
function gate() {
  let release;
  const promise = new Promise((resolve) => {
    release = resolve;
  });
  return { promise, release };
}

// holdLines は /api/lines をロケールごとに栓で止める。release(locale) を呼ぶまで、その
// ロケールの応答を返さない。asked は要求の来たロケールを来た順に控える。fail に入れた
// ロケールは、栓を開けたあと 500 で返す。応答が返る順を試験の側で決めるために使う。
async function holdLines(page, { fail = [] } = {}) {
  const gates = new Map();
  const gateFor = (locale) => {
    if (!gates.has(locale)) {
      gates.set(locale, gate());
    }
    return gates.get(locale);
  };
  const asked = [];
  await page.route(isPath("/api/lines"), async (route) => {
    const locale = new URL(route.request().url()).searchParams.get("locale");
    asked.push(locale);
    await gateFor(locale).promise;
    if (fail.includes(locale)) {
      await route.fulfill({ status: 500, body: "boom" });
      return;
    }
    await route.continue();
  });
  return { asked, release: (locale) => gateFor(locale).release() };
}

// watchLinesSettled は、画面が /api/lines の応答を受け取り終えたロケールを頁の中で控える
// 仕掛けを入れる。goto より前に呼ぶ。控えたものは linesSettled で読む。
//
// 古い応答を描かないことは、画面がその応答を受け取り終えたあとでないと確かめられない。
// 描かないのが正しいので、画面には待つ手がかりが出ない。response の事象は試験の側へ先に
// 届くことがあり、そのとき load の .then はまだ走っていないかもしれない。ここでは、画面が
// 本文を読み終えた（失敗なら状態を受け取った）あと、次のタスクで控える。そこまでは
// マイクロタスクだけでつながっているので、控えた時点で load の .then か .catch は走り
// 終えている。タスクは MessageChannel で積む。setTimeout で積むと、page.clock で時計を
// 止めた試験では控えが走らない。
async function watchLinesSettled(page) {
  await page.addInitScript(() => {
    const send = window.fetch;
    const channel = new MessageChannel();
    window.__linesSettled = [];
    channel.port1.onmessage = (event) => {
      window.__linesSettled.push(event.data);
    };
    window.fetch = function (input) {
      return send.apply(this, arguments).then((res) => {
        const url = new URL(String(input), window.location.href);
        if (url.pathname !== "/api/lines") {
          return res;
        }
        const settled = () => channel.port2.postMessage(url.searchParams.get("locale"));
        if (!res.ok) {
          settled();
          return res;
        }
        const read = res.json.bind(res);
        res.json = () =>
          read().then((data) => {
            settled();
            return data;
          });
        return res;
      });
    };
  });
}

// linesSettled は、ここまでに画面が受け取り終えた /api/lines の応答のロケール（来た順）。
function linesSettled(page) {
  return page.evaluate(() => window.__linesSettled);
}

// answerDialogs はダイアログを控え、answers の順に受ける（true）か断る（false）。
function answerDialogs(page, answers) {
  const seen = [];
  const queue = [...answers];
  page.on("dialog", async (dialog) => {
    seen.push({ type: dialog.type(), message: dialog.message() });
    if (queue.shift()) {
      await dialog.accept();
    } else {
      await dialog.dismiss();
    }
  });
  return seen;
}

// countRowPosts は /api/rows への POST を頁の中で数える仕掛けを入れる。goto より前に呼ぶ。
//
// 「送らない」ことを確かめるのに、request の事象を数えるだけでは足りない。事象は
// 試験の側へ遅れて届くので、数えた時点ではまだ届いていないだけかもしれない。頁の中で
// 数えれば、時計を進め終えた時点で送ったかどうかが決まっている（flush は時計の中から
// 同期で fetch を呼ぶ）。conflict.spec.mjs の openPaused と同じやり方。
async function countRowPosts(page) {
  await page.addInitScript(() => {
    const send = window.fetch;
    window.__rowPosts = 0;
    window.fetch = function (input, init) {
      if (init && init.method === "POST" && String(input).endsWith("/api/rows")) {
        window.__rowPosts += 1;
      }
      return send.apply(this, arguments);
    };
  });
}

// rowPosts は、ここまでに画面が /api/rows へ送った POST の数（countRowPosts が数えたもの）。
function rowPosts(page) {
  return page.evaluate(() => window.__rowPosts);
}

// chipBox は条件のチップのチェックボックス。label は画面に出る名前。
function chipBox(page, label) {
  return page.locator("#filters label.chip").filter({ hasText: label }).locator('input[type="checkbox"]');
}

// failRow は行 n に value を打ち、待ち受けに行ごとに断らせて「保存できない行」にする。
//
// 要求のキーをファイルに無いものへ差し替えて送る。待ち受けは 422 と行ごとの理由を
// 返し、ファイルへは1バイトも書かない。画面は値を控えたまま自動保存の対象から外すので、
// 時計が動いても状態が変わらない（送り直しの時計を回さない）。
async function failRow(page, n, value) {
  await page.route(isPath("/api/rows"), async (route) => {
    const body = JSON.parse(route.request().postData() ?? "{}");
    body.edits = (body.edits ?? []).map((edit) => ({ ...edit, key: wrongKey }));
    await route.continue({ postData: JSON.stringify(body) });
  });
  await typeTranslation(page, n, value);
  // Escape は閉じるだけで、閉じるときに保存へ回る。
  await editor(page).press("Escape");
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_failed"));
  await expect(rowByLine(page, n)).toHaveClass(/(^|\s)save-failed(\s|$)/);
  await expect(translationCell(page, n)).toHaveText(value);
}

// raiseConflict は goodbye の行に typed を打ち、送る前によそがその行の訳を external へ書き換えた
// 状態で送って、競合の引き止めを出す（conflict.spec.mjs の同じ名前の道具と同じ手順）。待ち受けが
// 409 を返したことまで確かめ、よそが書いたあとのファイルのバイトを返す。打ってからよそが
// 書き換えるまでに自動保存の時計が切れると 409 にならないので、時計を止めた頁で使う。
async function raiseConflict(page, server, external) {
  const text = await server.readRootText(workingRel);
  const row = `,${SAMPLE.goodbye.source},${SAMPLE.goodbye.ja}\n`;
  expect(text).toContain(row);
  await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
  await server.writeRoot(workingRel, text.replace(row, `,${SAMPLE.goodbye.source},${external}\n`));
  const saving = page.waitForResponse(
    (res) => new URL(res.url()).pathname === "/api/rows" && res.request().method() === "POST",
  );
  await editor(page).press("Escape");
  expect((await saving).status()).toBe(409);
  await expect(page.locator("#conflict")).toBeVisible();
  return server.readRoot(workingRel);
}

test.describe("画面の言語", () => {
  // 目録から入る文言の代表。どれも index.html では空で、app.js の applyCatalog が埋める。
  const texts = [
    ["#app-title", "app.title"],
    ["#locale-label", "ui.locale"],
    ["#reload-label", "ui.reload"],
    ["#col-line", "ui.col_line"],
    ["#col-status", "ui.col_status"],
    ["#col-speaker", "ui.col_speaker"],
    ["#col-source", "ui.col_source"],
    ["#col-translation", "ui.edit_label"],
    ["#edit-notice-text", "ui.edit_notice"],
    ["#finder-title", "ui.finder_title"],
    ["#filter-label", "ui.filter"],
    ["#filter-clear-label", "ui.filter_clear"],
    ["#keys-title", "ui.keys_title"],
    ["#keys", "ui.keys_help"],
    ["#finder-note-title", "ui.finder_note_title"],
    ["#finder-note", "ui.finder_note"],
    ["#export-title", "ui.export_title"],
    ["#panel-more", "ui.panel_more"],
    ["#counts-title", "ui.counts"],
    ["#stats-title", "ui.stats"],
    ["#conflict-title", "ui.conflict_title"],
    ["#orphans-title", "ui.orphans_title"],
    ["#save-state", "ui.save_clean"],
  ];
  // 名前を属性で持つもの（アイコンだけのボタンと、検索の欄）。
  const attrs = [
    ["#menu", "aria-label", "ui.sidebar"],
    ["#sidebar-close", "aria-label", "ui.sidebar_close"],
    ["#search", "aria-label", "ui.search"],
    ["#search", "placeholder", "ui.search_placeholder"],
  ];

  // expectCatalog は画面が lang の目録で組まれていることを確かめる。
  async function expectCatalog(page, lang) {
    const { lang: htmlLang, dir } = meta(lang);
    await expect(page.locator("html")).toHaveAttribute("lang", htmlLang);
    await expect(page.locator("html")).toHaveAttribute("dir", dir);
    for (const [selector, key] of texts) {
      await expect(page.locator(selector), selector).toHaveText(msg(lang, key));
    }
    for (const [selector, name, key] of attrs) {
      await expect(page.locator(selector), `${selector} の ${name}`).toHaveAttribute(name, msg(lang, key));
    }
    await expect(page.locator("#rows")).toHaveText(msg(lang, "ui.rows", { count: 3 }));
    // 断り書きは待ち受けが組む（/api/lines）。画面と同じ言語で来なければ、1画面に2言語が混ざる。
    await expect(page.locator("#notes li").first()).toHaveText(
      msg(lang, "note.via_publish", { path: "Translations/ja/strings.csv" }),
    );
  }

  for (const lang of ["ja", "en"]) {
    test.describe(`--ui-lang ${lang} で起動したとき`, () => {
      // ブラウザーは設定の既定（ja-JP）のまま。--ui-lang はそれより強い。
      test.use({ dwloc: { uiLang: lang } });

      // 画面の中に言語の切り替えは無い（README）。--ui-lang が効かないと、翻訳者は
      // 読めない言語の画面から抜け出す手段を持たない。html の lang は読み上げと字形の
      // 選び方を決めるので、文言と一緒に変わらなければならない。
      test("文言と html の lang・dir がその言語の目録になる", async ({ app }) => {
        await expectCatalog(app, lang);
      });
    });
  }

  test.describe("--ui-lang en でブラウザーが日本語を求めるとき", () => {
    test.use({ dwloc: { uiLang: "en" }, locale: "ja-JP" });

    // 英語の画面に日本語の文言が1つでも残ると、それは目録を通らずに書かれた文字列である
    // （「文言を1つも持たない」の破れ）。行の中身（ja の訳）は日本語でよいので一覧は外し、
    // 目録と待ち受けの文だけで組まれる場所を見る。閉じた畳みの中身も textContent で見る。
    test("行の中身のほかには日本語を1字も出さない", async ({ app }) => {
      await expect(app.locator("#stats li")).not.toHaveCount(0);
      const texts = await app.evaluate(() =>
        [".top", "#sidebar", ".thead", "#edit-notice"].map((sel) => [sel, document.querySelector(sel).textContent]),
      );
      for (const [selector, text] of texts) {
        expect(text, selector).not.toMatch(/[\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Han}、。「」『』（）：]/u);
      }
    });
  });

  // --ui-lang を省いたときは Accept-Language で決まり、当たらなければ英語（internal/web の
  // forRequest）。英語へ落とすのは、目録の無い言語の翻訳者にとって日本語より読める
  // 見込みが高いからで、日本語へ落とすと README の「ja が当たらないと英語」とも食い違う。
  for (const [browser, lang] of [
    ["en-US", "en"],
    ["ja-JP", "ja"],
    ["de-DE", "en"],
  ]) {
    test.describe(`--ui-lang を省いてブラウザーが ${browser} のとき`, () => {
      test.use({ dwloc: { uiLang: "" }, locale: browser });

      test(`${lang} の目録で組む`, async ({ app }) => {
        await expectCatalog(app, lang);
      });
    });
  }
});

// 題名はブラウザーの履歴に残る（app.js 冒頭「題名に行の中身を入れない」）。行の中身が
// 入ると、原文（再配布しない英語の台本）が履歴という別の場所へ漏れる。ロケールを
// 切り替えても題名が変わらないことも見る。変わるなら、そこに何かを差し込む道がある。
test("題名は目録の app.title だけで、ロケールを切り替えても変わらない", async ({ app }) => {
  const title = msg("ja", "app.title");
  await expect(app).toHaveTitle(title);
  await app.locator("#locale").selectOption("he");
  await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 1 }));
  await expect(app).toHaveTitle(title);
});

// トークンは最初の1回だけ URL に載る。303 で / へ移して Cookie に替えるので、アドレス欄と
// 履歴にはトークンの無い URL しか残らない。残ると、履歴を見た人がこの待ち受けに書き込める。
test("トークン付きの URL を開くと 303 で / へ移り、URL にトークンが残らない", async ({ page, server }) => {
  const statuses = [];
  page.on("response", (res) => {
    if (res.request().isNavigationRequest()) {
      statuses.push([new URL(res.url()).search, res.status()]);
    }
  });
  await openApp(page, server);
  expect(page.url()).toBe(`${server.origin}/`);
  expect(page.url()).not.toContain(server.token);
  expect(statuses).toEqual([
    [`?t=${server.token}`, 303],
    ["", 200],
  ]);
});

// 翻訳者は画面を F5 で開き直す。トークンはもう URL に無いので、Cookie だけで入れなければ
// ならない。入れないと、訳している途中の頁を開き直した瞬間に締め出される。
test("トークンの無い / のまま開き直しても、同じ画面が組み上がる", async ({ app, server }) => {
  await app.reload();
  await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
  await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
  expect(app.url()).toBe(`${server.origin}/`);
});

// ロケール名はデータ側の語彙なので訳さずにそのまま出す（fillLocales）。並びと中身は
// 待ち受けが返した一覧のままで、画面では足さない。指定して起動したときは「選んで
// ください」を入れない。入れると、行が並んでいるのに未選択へ戻せる選択肢が1つ増える。
test("ロケールの選択肢は待ち受けが返した一覧で、起動時に指定したロケールを選んでおく", async ({ app }) => {
  const options = app.locator("#locale option");
  await expect(options).toHaveText(["he", "ja"]);
  await expect(app.locator("#locale")).toHaveValue("ja");
  await expect(app.locator('#locale option[value=""]')).toHaveCount(0);
});

// 閉じたロケールの欄に焦点を置いて矢印キーを押すと、Windows と Linux のブラウザーは押す
// たびに change を出す。以前はそのたびに読みにいき、選び終える前の途中のロケールを1つずつ
// 読んだ（未保存の訳があれば、そのたびに「切り替えると消えます」と尋ねた）。最後の change
// から少し（app.js の localeDelay）待ってから、そのとき選ばれているロケールだけを読む。
// 見本にロケールを1つ足し、he / ja / ko の3つにして、ja から ko を経て he まで動かす。
test.describe("ロケールの欄を矢印キーで動かしたとき", () => {
  const koRel = "Translations/ko/strings.csv";
  const repo = sampleRepo();
  test.use({
    repo: {
      ...repo,
      root: { ...repo.root, [koRel]: publishedFile(["", { ...SAMPLE.hello, translation: "안녕?" }, ""]) },
    },
  });
  // openPaused は偽の時計を入れてから画面を開き、開き終えたところで時計を止める。
  async function openPaused(page, server) {
    await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
    await openApp(page, server);
    await page.clock.pauseAt(new Date("2026-01-01T01:00:00Z"));
  }

  // asked は /api/lines に尋ねたロケールの並び。
  function asked(requests) {
    return requests.map((req) => new URL(req.url()).searchParams.get("locale"));
  }

  test("続けて動かしても、途中のロケールは読まず、止めたところのロケールだけを読む", async ({ page, server }) => {
    await openPaused(page, server);
    const lines = watchRequests(page, "/api/lines");
    await page.locator("#locale").focus();
    await page.keyboard.press("ArrowDown");
    await expect(page.locator("#locale")).toHaveValue("ko");
    await page.keyboard.press("ArrowUp");
    await page.keyboard.press("ArrowUp");
    await expect(page.locator("#locale")).toHaveValue("he");

    // 最後に動かしてから localeDelay たつまでは読みにいかない。
    await page.clock.runFor(localeDelay - 1);
    expect(asked(lines)).toEqual([]);
    await expect(page.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));

    await page.clock.runFor(1);
    await expect(page.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
    await expect(page.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 1 }));
    expect(asked(lines)).toEqual(["he"]);
  });

  // 行って戻っただけなら、切り替えていない。読み直すと、条件と検索語が外れ、未保存の訳が
  // あれば「切り替えると消えます」と尋ねる。
  test("元のロケールへ戻しただけなら、読みにいかず、検索語も外さない", async ({ page, server }) => {
    await openPaused(page, server);
    await page.locator("#search").fill("Hello");
    await page.clock.runFor(200);
    await expect(page.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));
    const lines = watchRequests(page, "/api/lines");
    const dialogs = watchDialogs(page, true);
    // 保存を落として、未保存の訳を抱えた状態にする（切り替えるなら尋ねる状態）。
    await page.route(isPath("/api/rows"), (route) => route.abort("connectionrefused"));
    await typeTranslation(page, SAMPLE_LINES.hello, typed);
    await page.locator("#locale").focus();
    await expect(saveState(page)).toHaveClass(/(^|\s)failed(\s|$)/);
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("ArrowUp");
    await expect(page.locator("#locale")).toHaveValue("ja");

    await page.clock.runFor(localeDelay * 2);
    expect(asked(lines)).toEqual([]);
    expect(dialogs).toEqual([]);
    await expect(page.locator("#search")).toHaveValue("Hello");
    await expect(page.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));
  });

  // 待っているあいだに読み直しを押したら、あとから押した読み直しを採る。待っていた切り替えが
  // あとから走ると、読み直しを押したのに別のロケールへ移る。
  test("選んで待つあいだに読み直しを押すと、切り替えをやめて、出ているロケールを読み直す", async ({
    page,
    server,
  }) => {
    await openPaused(page, server);
    const lines = watchRequests(page, "/api/lines");
    await page.locator("#locale").selectOption("ko");
    await page.locator("#reload").click();
    await expect(page.locator("#locale")).toHaveValue("ja");
    await expect.poll(() => asked(lines)).toEqual(["ja"]);
    await expect(page.locator("#list")).toHaveAttribute("aria-busy", "false");

    await page.clock.runFor(localeDelay * 2);
    expect(asked(lines)).toEqual(["ja"]);
    await expect(page.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
    await expect(page.locator("#locale")).toHaveValue("ja");
  });
});

test.describe("ロケールを省いて起動したとき", () => {
  test.use({ dwloc: { locale: "" } });

  // 省くのは既定の経路（ダブルクリックで開いたときもこれ）。選ぶ前に「ファイル」の畳みを
  // 出すと、名前の入っていない畳みが焦点の順に残る（render の注記）。選ぶ前にどれかの
  // ロケールを読みにいくと、翻訳者が決めていないロケールの一覧を直させることになる。
  test("選ぶまで行もファイルの畳みも出さず、/api/lines も叩かない", async ({ page, server }) => {
    const lines = watchRequests(page, "/api/lines");
    await openApp(page, server);
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.select_locale"));
    // 選ぶ前の案内であって失敗ではない。失敗の出し方（.error）では出さない。
    await expect(page.locator("#message")).toHaveClass(/(^|\s)info(\s|$)/);
    await expect(page.locator("#message")).not.toHaveClass(/(^|\s)error(\s|$)/);
    await expect(page.locator("#locale option")).toHaveText([msg("ja", "ui.select_locale"), "he", "ja"]);
    await expect(page.locator("#locale")).toHaveValue("");
    await expect(dataRows(page)).toHaveCount(0);
    await expect(page.locator("#rows")).toBeEmpty();
    await expect(page.locator("#panel-fold")).toHaveJSProperty("hidden", true);
    expect(lines).toHaveLength(0);

    await page.locator("#locale").selectOption("ja");
    await expect(page.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
    await expect(page.locator("#panel-fold")).toHaveJSProperty("hidden", false);
    await expect(page.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
    await expect(page.locator("#message")).toBeEmpty();
    expect(lines).toHaveLength(1);
  });

  // 選んだあとも空の選択肢が残っていたころは、空へ戻すと一覧だけが消え、「表示中 N 行」・
  // ファイルの名前・条件のチップ・抱えている訳は前のロケールのまま残った。「表示中 N 行」は、
  // いま画面に出ている行数を言う唯一の場所である（doc.go「出ている行数は件数と混ぜない」、
  // index.html の #shown）。選んだあとは、起動時に指定したときと同じ並びにして（上の
  // 「ロケールの選択肢は待ち受けが返した一覧で…」）、戻す道そのものを作らない（app.js の load）。
  test("選んだあとは空の選択肢を外し、前のロケールの表示を残したまま空へ戻す道を作らない", async ({ app }) => {
    await app.locator("#locale").selectOption("ja");
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 3 }));
    await expect(app.locator("#locale option")).toHaveText(["he", "ja"]);
    await expect(app.locator("#locale")).toHaveValue("ja");
    await expect(app.locator('#locale option[value=""]')).toHaveCount(0);

    // 外したあとも、ほかのロケールへは今までどおり切り替えられる。
    await app.locator("#locale").selectOption("he");
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
    await expect(app.locator("#locale option")).toHaveText(["he", "ja"]);
  });

  // 空の選択肢を外すのは読めたときだけにする。最初の選択で読めなかったときに外すと、
  // 欄は空（load の catch が state.locale へ戻す）なのに、それを表す選択肢が無くなる。
  test("最初に選んだロケールを読めなければ、選択を空へ戻し、空の選択肢も残す", async ({ app, server }) => {
    await rm(server.rootPath(heRel));
    await app.locator("#locale").selectOption("he");
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    await expect(app.locator("#locale")).toHaveValue("");
    await expect(app.locator("#locale option")).toHaveText([msg("ja", "ui.select_locale"), "he", "ja"]);
    await expect(dataRows(app)).toHaveCount(0);

    await app.locator("#locale").selectOption("ja");
    await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
    await expect(app.locator("#locale option")).toHaveText(["he", "ja"]);
  });

  // 最初の読み込みが返る前にもう1つ選んだとき（選んでから読みにいくまでの待ち、app.js の
  // localeDelay より長く置いてから選び直し、そのあいだに応答が返っていないと起きる。待ちが
  // 入る前は、欄に焦点を置いて↓を続けて押すだけで起きた）。
  // 以前は、先に返った応答が選択肢を組み直して欄をそのロケールにし、空の選択肢が消えた
  // あとに返った応答は欄に触れずに一覧だけを描いた。欄と、一覧・ファイルの名前・保存の
  // 宛先（state.locale）が別のロケールを指した。欄に出ているロケールを選び直しても change
  // は起きないので、欄が指すロケールへは欄からたどり着けない。描くのは最後に始めた読み込み
  // だけにし、欄は描いたロケールにそろえる（app.js の load）。応答が返る順はどちらもある。
  for (const [label, order] of [
    ["選んだ順", ["ja", "he"]],
    ["逆の順", ["he", "ja"]],
  ]) {
    test(`続けて2つ選んで応答が${label}に返っても、あとで選んだロケールだけを描き、欄もそれにそろえる`, async ({
      page,
      server,
    }) => {
      await watchLinesSettled(page);
      await openApp(page, server);
      const lines = await holdLines(page);
      await page.locator("#locale").selectOption("ja");
      // 選んだロケールを読みにいってから（localeDelay のあと）選び直す。待たずに選び直すと、
      // 画面は最後に選んだほうだけを読む（「ロケールの欄を矢印キーで動かしたとき」）。
      await expect.poll(() => lines.asked).toEqual(["ja"]);
      await page.locator("#locale").selectOption("he");
      await expect.poll(() => lines.asked.toSorted()).toEqual(["he", "ja"]);

      for (const locale of order) {
        lines.release(locale);
        await expect.poll(() => linesSettled(page)).toContain(locale);
      }
      await expect(page.locator("#locale")).toHaveValue("he");
      await expect(page.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
      await expect(page.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 1 }));
      await expect(page.locator("#message")).toBeEmpty();
      await expect(page.locator("#locale option")).toHaveText(["he", "ja"]);
    });
  }

  // 読み直すのは画面に出ているロケールで、欄の値ではない（app.js の読み直し）。まだ何も
  // 出ていないときは読み直すものが無いので、何もしない。最初の読み込みが返る前に押しても、
  // 選んだロケールの読み込みを取りやめて「ロケールを選んでください」へ戻したりしない。
  test("最初の読み込みが返る前に読み直しを押しても、選んだロケールをそのまま読み終える", async ({ app }) => {
    const lines = await holdLines(app);
    await app.locator("#locale").selectOption("ja");
    await expect.poll(() => lines.asked).toEqual(["ja"]);
    await app.locator("#reload").click();
    await expect(app.locator("#locale")).toHaveValue("ja");

    lines.release("ja");
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
    await expect(app.locator("#locale")).toHaveValue("ja");
    await expect(app.locator("#message")).toBeEmpty();
    expect(lines.asked).toEqual(["ja"]);
  });
});

// 畳みの summary は焦点を受ける。文言が入る前に出すと、名前の無い開閉要素が焦点の順に
// 並ぶ（index.html の注記）。「ファイル」の畳みだけは summary の主役がファイルの名前なので、
// 目録ではなく /api/lines を受けてから出す（applyCatalog と render の注記）。
test("畳みは目録が届いたら出し、ファイルの畳みだけは行を読んでから出す", async ({ page, server }) => {
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  await page.route(isPath("/api/lines"), async (route) => {
    await gate;
    await route.continue();
  });
  await page.goto(server.url);

  // 目録は届いて、行はまだ届いていない。
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_clean"));
  await expect(page.locator("#message")).toHaveText(msg("ja", "ui.loading"));
  // 書き出しの入口は帯のボタン（#export-open）で、畳みと同じく文言が入ったら出す。
  for (const id of ["#keys-fold", "#finder-fold", "#export-open"]) {
    await expect(page.locator(id), id).toHaveJSProperty("hidden", false);
  }
  await expect(page.locator("#keys-title")).toHaveText(msg("ja", "ui.keys_title"));
  await expect(page.locator("#finder-note-title")).toHaveText(msg("ja", "ui.finder_note_title"));
  await expect(page.locator("#export-title")).toHaveText(msg("ja", "ui.export_title"));
  await expect(page.locator("#panel-fold")).toHaveJSProperty("hidden", true);
  await expect(page.locator("#file-path")).toBeEmpty();
  await expect(page.locator("#rows")).toBeEmpty();

  release();
  await expect(page.locator("#panel-fold")).toHaveJSProperty("hidden", false);
  await expect(page.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
  await expect(page.locator("#message")).toBeEmpty();
  await expect(page.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
});

// 目録が取れないと、画面は1文字も訳せない。それでも「黙って失敗しない」ので、失敗は
// 帯に出す。畳みは名前が入らないので出さない。行も読みにいかない（どのロケールかも
// 分からない）。
test("目録を取れないときも黙らず、名前の無い畳みを出さず、行を読みにいかない", async ({ page, server }) => {
  await page.route(isPath("/api/bootstrap"), (route) =>
    route.fulfill({ status: 500, contentType: "text/plain", body: "boom" }),
  );
  const lines = watchRequests(page, "/api/lines");
  await page.goto(server.url);
  await expect(page.locator("#message")).not.toBeEmpty();
  // 名前の入らない帯のボタン（#export-open）も出さない。名前の無い焦点の止まり場になる。
  for (const id of ["#keys-fold", "#finder-fold", "#export-open", "#panel-fold"]) {
    await expect(page.locator(id), id).toHaveJSProperty("hidden", true);
  }
  await expect(page.locator("#locale option")).toHaveCount(0);
  await expect(dataRows(page)).toHaveCount(0);
  expect(lines).toHaveLength(0);
});

// 目録が無いと t() は鍵をそのまま返すので、以前は帯に「ui.load_failed」という鍵が出た。
// 翻訳者には何のことか分からない。この1文だけは画面が日英の固定の文を持つ（app.js の
// bootFailed。「文言を1つも持たない」約束の、ただ1つの例外）。どちらの言語の画面かは
// 目録が決めるので、目録が無いときは分からない。だから両方を、それぞれの lang を付けて
// 並べる。失敗なので、失敗の出し方（.error）で出す。
test("目録を取れないときは、鍵ではなく日英の固定の文を失敗として出す", async ({ page, server }) => {
  await page.route(isPath("/api/bootstrap"), (route) =>
    route.fulfill({ status: 500, contentType: "text/plain", body: "boom" }),
  );
  await page.goto(server.url);
  const message = page.locator("#message");
  await expect(message).not.toBeEmpty();
  await expect(message).not.toContainText("ui.");
  await expect(message).toHaveClass(/(^|\s)error(\s|$)/);
  for (const lang of ["ja", "en"]) {
    await expect(message.locator(`[lang="${lang}"]`), lang).not.toBeEmpty();
  }
  // 日本語の側には日本語が、英語の側には日本語が1字も無い。
  await expect(message.locator('[lang="ja"]')).toHaveText(/[぀-ヿ]/);
  await expect(message.locator('[lang="en"]')).not.toHaveText(/[぀-ヿ一-鿿]/);
});

// 「読み込んでいます…」と「ロケールを選んでください。」は失敗ではない。以前は #message の
// class が失敗の出し方（notice error）に固定されていて、ふつうに起動するたびに、目録と行が
// 届くまでのあいだ赤い帯が出た。失敗を知らせる帯と同じ見た目だと、翻訳者は毎回何かが
// 壊れたと読む。案内は案内の出し方（.info）、失敗だけを失敗の出し方で出す。
test("読み込み中の案内は失敗の帯で出さず、読み込めなかったときだけ失敗の帯で出す", async ({ page, server }) => {
  // 起動の最初から #message の移り変わりを控える。赤い帯が一瞬でも出たかを見る。
  await page.addInitScript(() => {
    window.__messages = [];
    document.addEventListener("DOMContentLoaded", () => {
      const node = document.getElementById("message");
      const note = () => window.__messages.push({ text: node.textContent, cls: node.className });
      new MutationObserver(note).observe(node, { attributes: true, childList: true, subtree: true, characterData: true });
    });
  });
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  await page.route(isPath("/api/lines"), async (route) => {
    await gate;
    await route.continue();
  });
  await page.goto(server.url);
  const message = page.locator("#message");
  await expect(message).toHaveText(msg("ja", "ui.loading"));
  await expect(message).toHaveClass(/(^|\s)info(\s|$)/);
  await expect(message).not.toHaveClass(/(^|\s)error(\s|$)/);
  release();
  await expect(page.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
  await expect(message).toBeEmpty();
  const seen = await page.evaluate(() => window.__messages);
  expect(seen.filter((m) => m.text !== "" && /(^|\s)error(\s|$)/.test(m.cls))).toEqual([]);

  // 読み込めなかったときは失敗の出し方になる。
  await page.unroute(isPath("/api/lines"));
  await page.route(isPath("/api/lines"), (route) => route.fulfill({ status: 500, body: "boom" }));
  await page.locator("#reload").click();
  await expect(message).toHaveText(msg("ja", "ui.load_failed"));
  await expect(message).toHaveClass(/(^|\s)error(\s|$)/);
  await expect(message).not.toHaveClass(/(^|\s)info(\s|$)/);
});

test.describe("ゲームのフォルダー", () => {
  // --game が無いのに探し先の欄を出すと、どこも探していないのに「探している」と読まれる。
  test("--game が無ければ探し先の欄を隠して空にする", async ({ app }) => {
    await expect(app.locator("#game-path")).toHaveJSProperty("hidden", true);
    await expect(app.locator("#game-path")).toBeEmpty();
  });

  test.describe("--game を付けたとき", () => {
    test.use({ repo: sampleRepo({ game: true }) });

    // 探し先は起動時に決まり、画面の操作では変わらない（showGamePath の注記）。ロケールを
    // 替えて消えると、he では探したのに無かったのか、探していないのかが分からなくなる。
    test("ロケールを切り替えても探し先を出し続け、ファイルの欄だけが替わる", async ({ app, server }) => {
      const game = `${msg("ja", "ui.game_folder")}: ${server.game.replaceAll("\\", "/")}`;
      await expect(app.locator("#game-path")).toHaveText(game);
      await app.locator("#locale").selectOption("he");
      await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
      await expect(app.locator("#game-path")).toHaveText(game);
      await expect(app.locator("#game-path")).toHaveJSProperty("hidden", false);
    });
  });
});

// 画面で直した訳がどこへ入るのかは、断り書きにしか出ない。作業コピーを並べているときに
// 「publish を回すとコミットする側へ入る」と言わないと、翻訳者は画面の保存でコミットする
// 側が変わったと思う。公開ファイルそのものを並べているときに言うと、それは嘘になる。
test("作業コピーを並べているか公開ファイルを並べているかを、断り書きで言い分ける", async ({ app }) => {
  const notes = app.locator("#notes li");
  const viaPublish = msg("ja", "note.via_publish", { path: "Translations/ja/strings.csv" });
  await expect(notes.filter({ hasText: viaPublish })).toHaveCount(1);
  await expect(notes.filter({ hasText: msg("ja", "note.working_read", { path: workingRel }) })).toHaveCount(1);
  await expect(notes.filter({ hasText: msg("ja", "note.no_source") })).toHaveCount(0);

  await app.locator("#locale").selectOption("he");
  await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
  await expect(notes.filter({ hasText: msg("ja", "note.no_source") })).toHaveCount(1);
  await expect(
    notes.filter({ hasText: msg("ja", "note.working_none", { path: "Translations/_discovered/he.working.csv" }) }),
  ).toHaveCount(1);
  // he は入力と出力が同じファイル。publish を回せと言う理由が無い。
  await expect(notes.filter({ hasText: viaPublish })).toHaveCount(0);
  await expect(notes.filter({ hasText: msg("ja", "note.via_publish", { path: heRel }) })).toHaveCount(0);
});

// 数えたものは待ち受けが数えた値で、画面は並べるだけ（「判断を1つも持たない」）。ロケールを
// 替えたら描き直す。前のロケールの数が残ると、別のファイルの行数を読むことになる。
test("数えたものを『名前: 数』で並べ、ロケールを替えると描き直す", async ({ app }) => {
  const stats = app.locator("#stats li");
  const line = (key, value) => `${msg("ja", key)}: ${value}`;
  await expect(stats.filter({ hasText: line("stats.file_lines", 8) })).toHaveCount(1);
  await expect(stats.filter({ hasText: line("stats.data_lines", 3) })).toHaveCount(1);
  await expect(stats.filter({ hasText: line("stats.working_rows", 3) })).toHaveCount(1);

  await app.locator("#locale").selectOption("he");
  await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 1 }));
  await expect(stats.filter({ hasText: line("stats.file_lines", 5) })).toHaveCount(1);
  await expect(stats.filter({ hasText: line("stats.data_lines", 1) })).toHaveCount(1);
  // he に作業コピーは無い。作業コピーの行数を出すと、無いファイルを数えたことになる。
  await expect(stats.filter({ hasText: msg("ja", "stats.working_rows") })).toHaveCount(0);
});

test.describe("ヘッダーを受理できない作業コピー", () => {
  test.use({ repo: sampleRepo({ workingCopy: "a,b,c\n1,2,3\n" }) });

  // ヘッダーが読めないファイルの最終フィールドは訳とは限らない。書き換えさせると、訳でない
  // 列を壊す。理由はファイル全体の断り書きと行の両方に出す。出さないと、押しても開かない
  // 欄が黙って並ぶ。生の行はそのまま出す（直せなくても読めるようにする）。
  test("読み取り専用の理由を出し、行を編集させず、ファイルを1バイトも変えない", async ({ app, server }) => {
    const before = await server.readRoot(workingRel);
    const why = msg("ja", "reason.edit_bad_header", { line: 1, text: '"a,b,c"' });

    await expect(app.locator("#notes li").filter({ hasText: msg("ja", "ui.file_readonly", { reason: why }) })).toHaveCount(1);
    await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 1 }));

    const row = rowByLine(app, 2);
    await expect(row).toHaveClass(/(^|\s)not-editable(\s|$)/);
    await expect(row.locator(".cell.raw")).toHaveText("1,2,3");
    const note = row.locator(".cell.row-note");
    await expect(note).toBeVisible();
    await expect(note).toHaveText(msg("ja", "ui.not_editable", { reason: why }));
    await expect(note.locator('use[href="#i-lock"]')).toHaveCount(1);
    // 焦点を受ける欄が1つも無い（Tab でも入れない）。
    await expect(app.locator("#list .cell.translation[data-id]")).toHaveCount(0);
    await expect(app.locator("#list [tabindex]")).toHaveCount(0);

    await row.locator(".cell.raw").click();
    await expect(editor(app)).toHaveCount(0);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });
});

test.describe("読み直し", () => {
  // 読み直しは、よそ（ゲームの Export、別の窓の publish）が書き換えたファイルを見るための
  // 手段である。未保存が無いのに尋ねると、毎回「消えます」と脅されて押しにくくなる。
  test("未保存が無ければ尋ねずに、よそが書き換えたファイルの中身を並べ直す", async ({ app, server }) => {
    const dialogs = watchDialogs(app, false);
    const changed = "やあ";
    const text = await server.readRootText(workingRel);
    await server.writeRoot(workingRel, text.replace(`,${SAMPLE.hello.ja}\n`, `,${changed}\n`));

    const read = app.waitForResponse((res) => new URL(res.url()).pathname === "/api/lines");
    await app.locator("#reload").click();
    expect((await read).status()).toBe(200);
    await expect(translationCell(app, SAMPLE_LINES.hello)).toHaveText(changed);
    await expect(app.locator("#message")).toBeEmpty();
    expect(dialogs).toHaveLength(0);
  });

  // 読み直しは同じロケールを見続ける操作なので、翻訳者が決めた条件と検索語は残す
  // （boot の注記「読み直し（el.reload）では外さない」）。外すと、絞った一覧で作業して
  // いた人が、読み直すたびに1721行の先頭へ戻される。
  test("条件と検索語を外さずに当て直す", async ({ app }) => {
    const untranslated = chipBox(app, msg("ja", "category.untranslated"));
    await untranslated.check();
    await app.locator("#search").fill(SAMPLE.goodbye.speaker);
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));

    const read = app.waitForResponse((res) => new URL(res.url()).pathname === "/api/lines");
    await app.locator("#reload").click();
    await read;
    await expect(app.locator("#search")).toHaveValue(SAMPLE.goodbye.speaker);
    await expect(untranslated).toBeChecked();
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toBeVisible();
    await expect(rowByLine(app, SAMPLE_LINES.hello)).toBeHidden();
  });

  // 読み込みの失敗は、翻訳者にとって「読み直せばよい」一時の失敗である。そのとき一覧を
  // 消すと、直していた行も見ていた位置も失う。ファイルが戻れば、同じボタンで戻れること。
  test("失敗しても一覧を残して理由を出し、ファイルが戻れば読み直せる", async ({ app, server }) => {
    const bytes = await server.readRoot(workingRel);
    await rm(server.rootPath(workingRel));
    await app.locator("#reload").click();
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    await expect(dataRows(app).locator(".cell.num")).toHaveText(["5", "6", "7"]);
    await expect(app.locator("#locale")).toHaveValue("ja");
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));

    await server.writeRoot(workingRel, bytes);
    await app.locator("#reload").click();
    await expect(app.locator("#message")).toBeEmpty();
    await expect(dataRows(app).locator(".cell.num")).toHaveText(["5", "6", "7"]);
  });
});

test.describe("ロケールの切り替え", () => {
  // 前のロケールで決めた条件と検索語を持ち越すと、ヘッダーは「行: N」と言うのに一覧が
  // 空になる（boot の注記）。he の行には "Ryan" が当たらないので、外れていなければ 0 行になる。
  test("読めたら、前のロケールの条件と検索語を外す", async ({ app }) => {
    const untranslated = chipBox(app, msg("ja", "category.untranslated"));
    await untranslated.check();
    await app.locator("#search").fill(SAMPLE.goodbye.speaker);
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));

    await app.locator("#locale").selectOption("he");
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
    await expect(app.locator("#search")).toHaveValue("");
    await expect(app.locator('#filters input[type="checkbox"]:checked')).toHaveCount(0);
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));
    await expect(rowByLine(app, 4).locator(".cell.translation")).toHaveText(SAMPLE.hello.he);
  });

  // 切り替えの読み込みが返るまでのあいだも、前のロケールの行は出ている。以前はそこで打てたが、
  // 読めた時点で抱えている訳ごと片付くので、打った訳は黙って消えた（app.js の load）。保存が
  // 落ちていれば、ファイルにも画面にも残らず、保存の欄は「保存済み」になった。未保存が無く
  // 尋ねずに切り替えるときも、読み終えるまでは前のロケールの行を開かせない。読めたら、新しい
  // ロケールの行は読んだ版のまま打って保存できる。
  //
  // 読み込みのあいだに保存を送る道が無くなったので、送りかけの保存の応答が切り替えのあとに
  // 返ることは、画面の操作では起きなくなった。flush の世代の見分けは、その守りとして残してある。
  test("尋ねずに切り替えても、読み込みが返るまで前のロケールの行を開かせず、読めたら新しいロケールで打てる", async ({
    app,
    server,
  }) => {
    const before = await server.readRoot(workingRel);
    const lines = await holdLines(app);
    const rows = watchRequests(app, "/api/rows");

    await app.locator("#locale").selectOption("he");
    await expect.poll(() => lines.asked).toEqual(["he"]);
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.loading"));
    await expect(app.locator("#list")).toHaveAttribute("aria-busy", "true");
    await translationCell(app, SAMPLE_LINES.goodbye).click();
    await expect(editor(app)).toHaveCount(0);

    lines.release("he");
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
    await expect(app.locator("#list")).toHaveAttribute("aria-busy", "false");
    await expect(app.locator("#message")).toBeEmpty();
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
    expect(rows).toHaveLength(0);
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);

    // he で打てば、読んだ版のまま保存できる。
    const next = app.waitForResponse((res) => new URL(res.url()).pathname === "/api/rows");
    await typeTranslation(app, 4, `${SAMPLE.hello.he}!`);
    await editor(app).press("Escape");
    expect((await next).status()).toBe(200);
    await waitForSaved(app);
    expect(await server.readRootText(heRel)).toContain(`,${SAMPLE.hello.he}!`);
  });

  // 失敗したときに条件だけ外すと、チップと一覧が前のロケールのまま、欄だけが新しい
  // ロケールを指す三者バラバラの画面になった（load の注記。実際に起きた）。失敗したら
  // 何も変わっていないのが正しい。
  test("読めなければ、選択も一覧も条件も検索語も前のロケールのまま残す", async ({ app, server }) => {
    const dialogs = watchDialogs(app, false);
    const untranslated = chipBox(app, msg("ja", "category.untranslated"));
    await untranslated.check();
    await app.locator("#search").fill(SAMPLE.goodbye.speaker);
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));

    await rm(server.rootPath(heRel));
    await app.locator("#locale").selectOption("he");
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    await expect(app.locator("#locale")).toHaveValue("ja");
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
    await expect(app.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
    await expect(app.locator("#search")).toHaveValue(SAMPLE.goodbye.speaker);
    await expect(untranslated).toBeChecked();
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toBeVisible();
    expect(dialogs).toHaveLength(0);
  });

  // 読み込みが返る前に選び直し、前の読み込みがあとから返ったとき。描けば、欄はあとで
  // 選んだロケール、一覧とファイルの名前は前のロケールになる。失敗して返ったときに
  // 「読めませんでした」を出せば、読めている画面の上に嘘の失敗が載る。描くのも失敗を
  // 出すのも、最後に始めた読み込みだけにする（app.js の load）。
  for (const [label, fail] of [
    ["読めて", []],
    ["失敗して", ["he"]],
  ]) {
    test(`読み込みが返る前に選び直したら、前の読み込みがあとから${label}返っても画面を変えない`, async ({
      page,
      server,
    }) => {
      await watchLinesSettled(page);
      await openApp(page, server);
      const lines = await holdLines(page, { fail });
      await page.locator("#locale").selectOption("he");
      // he を読みにいってから（app.js の localeDelay のあと）選び直す。
      await expect.poll(() => lines.asked).toEqual(["he"]);
      await page.locator("#locale").selectOption("ja");
      await expect.poll(() => lines.asked.toSorted()).toEqual(["he", "ja"]);

      // 起動したときに読んだ ja のあとに、あとで選んだ ja が返る。
      lines.release("ja");
      await expect.poll(() => linesSettled(page)).toEqual(["ja", "ja"]);
      await expect(page.locator("#message")).toBeEmpty();
      lines.release("he");
      await expect.poll(() => linesSettled(page)).toEqual(["ja", "ja", "he"]);

      await expect(page.locator("#locale")).toHaveValue("ja");
      await expect(page.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
      await expect(page.locator("#rows")).toHaveText(msg("ja", "ui.rows", { count: 3 }));
      await expect(page.locator("#message")).toBeEmpty();
    });
  }
});

test.describe("読み込みが返るまで", () => {
  // 捨てると答えたあとの読み込みでは、flush が送らない（捨てると答えた訳をファイルに入れない）。
  // 以前はそのあいだも前の一覧で打てたので、答えたあとに打った新しい訳は送られず、読めた時点で
  // 捨てると答えた訳と一緒に消えた。ファイルにも画面にも残らず、保存の欄は「保存済み」になった
  // （app.js の load）。読み終えるまでは、マウスでも Tab でも入力欄を開かない。読み込んでいる
  // ことは画面に出し、打てそうな印（cursor: text）も下ろす。
  test("捨てると答えた読み直しの読み込みが返るまで、訳の欄をマウスでも Tab でも開かず、読めたらまた開ける", async ({
    app,
    server,
  }) => {
    const before = await server.readRoot(workingRel);
    await failRow(app, SAMPLE_LINES.goodbye, typed);
    const dialogs = watchDialogs(app, true);
    const lines = await holdLines(app);

    await app.locator("#reload").click();
    await expect.poll(() => dialogs.length).toBe(1);
    await expect.poll(() => lines.asked).toEqual(["ja"]);
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.loading"));
    await expect(app.locator("#list")).toHaveAttribute("aria-busy", "true");
    const hello = translationCell(app, SAMPLE_LINES.hello);
    await expect(hello).toHaveCSS("cursor", "progress");

    // マウスで押しても開かない。既定の動作は止めないので、焦点はその欄に入る。
    await hello.click();
    await expect(hello).toBeFocused();
    await expect(editor(app)).toHaveCount(0);
    // Tab で次の欄へ移っても開かない。
    await app.keyboard.press("Tab");
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toBeFocused();
    await expect(editor(app)).toHaveCount(0);

    lines.release("ja");
    await expect(app.locator("#message")).toBeEmpty();
    await expect(app.locator("#list")).toHaveAttribute("aria-busy", "false");
    await expect(hello).toHaveCSS("cursor", "text");
    // 捨てると答えた訳は捨てた。ファイルは1バイトも変わっていない。
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(SAMPLE.goodbye.ja);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
    // 読めたら、また開ける。
    await openEditor(app, SAMPLE_LINES.hello);
  });

  // 競合の引き止めのボタンも、読み終えるまで押させない（app.js の keepMine と takeFile）。以前は
  // 捨てると答えた読み直しの読み込みのあいだも押せた。「自分の訳を上に載せる」を押すと「読み込んで
  // います…」が消え、行と保存の欄は自分の訳を載せ直したように見えた。捨てると答えたあとなので
  // 送らず、読めた時点でそれも片付くので、押した訳はファイルにも画面にも残らず、保存の欄は
  // 「保存済み」になった。
  test("捨てると答えた読み直しの読み込みが返るまで、競合の引き止めのボタンを押させず、読めたら引き止めを下ろす", async ({
    page,
    server,
  }) => {
    await countRowPosts(page);
    await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
    await openApp(page, server);
    await page.clock.pauseAt(new Date("2026-01-01T01:00:00Z"));
    const external = "またね。";
    const conflicted = await raiseConflict(page, server, external);
    const asked = await rowPosts(page);
    const dialogs = watchDialogs(page, true);
    const lines = await holdLines(page);
    const keep = page.locator("#conflict-keep");
    const take = page.locator("#conflict-take");

    await page.locator("#reload").click();
    await expect.poll(() => dialogs.length).toBe(1);
    await expect.poll(() => lines.asked).toEqual(["ja"]);
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.loading"));
    await expect(keep).toBeDisabled();
    await expect(take).toBeDisabled();
    await expect(keep).toHaveCSS("cursor", "progress");
    // 押しても何も起きない。「読み込んでいます…」も引き止めも出たまま。
    await keep.click({ force: true });
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.loading"));
    await expect(page.locator("#conflict")).toBeVisible();
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_conflict"));

    lines.release("ja");
    await expect(page.locator("#conflict")).toBeHidden();
    await expect(page.locator("#message")).toBeEmpty();
    await expect(translationCell(page, SAMPLE_LINES.goodbye)).toHaveText(external);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_clean"));
    await page.clock.runFor(60_000);
    expect(await rowPosts(page)).toBe(asked);
    expect((await server.readRoot(workingRel)).equals(conflicted)).toBe(true);
  });

  // 読めなければ何も捨てていないので、引き止めのボタンはまた押せる。押したとおりに効く。
  test("競合したまま読み直しに失敗したら、引き止めのボタンをまた押せるように戻す", async ({ page, server }) => {
    await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
    await openApp(page, server);
    await page.clock.pauseAt(new Date("2026-01-01T01:00:00Z"));
    await raiseConflict(page, server, "またね。");
    const dialogs = watchDialogs(page, true);
    const lines = await holdLines(page, { fail: ["ja"] });
    const keep = page.locator("#conflict-keep");
    const take = page.locator("#conflict-take");

    await page.locator("#reload").click();
    await expect.poll(() => dialogs.length).toBe(1);
    await expect.poll(() => lines.asked).toEqual(["ja"]);
    await expect(keep).toBeDisabled();
    await expect(take).toBeDisabled();

    lines.release("ja");
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    await expect(page.locator("#conflict")).toBeVisible();
    await expect(keep).toBeEnabled();
    await expect(take).toBeEnabled();
    const saving = page.waitForResponse(
      (res) => new URL(res.url()).pathname === "/api/rows" && res.request().method() === "POST",
    );
    await keep.click();
    expect((await saving).status()).toBe(200);
    await waitForSaved(page);
    await expect(translationCell(page, SAMPLE_LINES.goodbye)).toHaveText(typed);
    expect(await server.readRootText(workingRel)).toContain(`,${SAMPLE.goodbye.source},${typed}\n`);
  });

  // 読めなかったら何も変わっていないのが正しい（load の注記）。一覧もまた編集できる。
  // 読み込みのあいだに開かせなかった欄も、押せば開く。
  //
  // 読み込みのあいだに Tab（やマウス）で焦点を載せた欄は、失敗したらそのまま開く。開かないと、
  // 焦点はその欄に残るのに focusin はもう来ないので、字も Enter も効かない。キーボードだけで
  // 打つ人は、Tab でいったん出て入り直すまで先へ進めない（nextEditable の注記が避けている
  // 「開けない行で行き止まる」形）。
  test("読み込みに失敗したら、一覧をまた編集できるように戻す", async ({ app, server }) => {
    const lines = await holdLines(app, { fail: ["ja"] });
    await app.locator("#reload").click();
    await expect.poll(() => lines.asked).toEqual(["ja"]);
    await expect(app.locator("#list")).toHaveAttribute("aria-busy", "true");
    await translationCell(app, SAMPLE_LINES.hello).click();
    await expect(editor(app)).toHaveCount(0);
    await app.keyboard.press("Tab");
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toBeFocused();
    await expect(editor(app)).toHaveCount(0);

    lines.release("ja");
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    await expect(app.locator("#list")).toHaveAttribute("aria-busy", "false");
    // 焦点の載っていた欄が開き、そのまま打てる。
    await expect(rowByLine(app, SAMPLE_LINES.goodbye).locator("textarea.editor")).toBeFocused();
    await app.keyboard.type(typed);
    await app.keyboard.press("Escape");
    await waitForSaved(app);
    expect(await server.readRootText(workingRel)).toContain(`,${SAMPLE.goodbye.source},${typed}\n`);

    // ほかの欄も、押せば開く。
    await typeTranslation(app, SAMPLE_LINES.hello, "もしもし。");
    await editor(app).press("Escape");
    await waitForSaved(app);
  });

  // 送り終えるのを待つあいだ（読み込みを始める前）は、まだ打てる。そこで開いた入力欄は、読み込みを
  // 始めるときに閉じる。開いたままだと、読み込みのあいだも打てる。打ってあった訳は、尋ねる前の
  // 送り直し（settle）で送られ、読み直した一覧にファイルの値として出る。
  test("送り終えるのを待つあいだに開いた入力欄は、読み込みを始めるときに閉じ、打ってあった訳は送る", async ({
    app,
    server,
  }) => {
    const added = "もしもし。";
    const hold = gate();
    await app.route(
      isPath("/api/rows"),
      async (route) => {
        await hold.promise;
        await route.continue();
      },
      { times: 1 },
    );
    const lines = await holdLines(app);
    const dialogs = watchDialogs(app, false);

    await typeTranslation(app, SAMPLE_LINES.goodbye, typed);
    await editor(app).press("Escape");
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_saving"));
    await app.locator("#reload").click();
    // まだ読みにいっていないので、開いて打てる。
    await typeTranslation(app, SAMPLE_LINES.hello, added);
    hold.release();

    await expect.poll(() => lines.asked).toEqual(["ja"]);
    await expect(editor(app)).toHaveCount(0);
    await expect(app.locator("#list")).toHaveAttribute("aria-busy", "true");
    lines.release("ja");
    await expect(app.locator("#message")).toBeEmpty();
    await waitForSaved(app);
    expect(dialogs).toHaveLength(0);
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    await expect(translationCell(app, SAMPLE_LINES.hello)).toHaveText(added);
    const text = await server.readRootText(workingRel);
    expect(text).toContain(`,${SAMPLE.goodbye.source},${typed}\n`);
    expect(text).toContain(`,${SAMPLE.hello.source},${added}\n`);
  });
});

test.describe("保存できていない訳があるとき", () => {
  // 切り替えと読み直しは、保存できていない訳を捨てる（load が state.pending / failed を
  // 空にする）。だから必ず尋ね、断られたら何1つ変えない（askDiscard）。欄だけ
  // 新しいロケールを指したり、要求が1つでも出たりすると、断ったのに訳が消える道になる。
  test("ロケールを切り替える前に尋ね、断れば何も変えない", async ({ app, server }) => {
    const before = await server.readRoot(workingRel);
    await failRow(app, SAMPLE_LINES.goodbye, typed);
    const dialogs = watchDialogs(app, false);
    const lines = watchRequests(app, "/api/lines");

    await app.locator("#locale").selectOption("he");
    await expect.poll(() => dialogs.length).toBe(1);
    // 切り替えのときは切り替えの文で尋ねる。「読み直しますか」と聞くと、何が起きるのかが
    // 読み取れない（読み直しの文 ui.discard_confirm とは鍵を分けてある）。
    expect(dialogs[0]).toEqual({ type: "confirm", message: msg("ja", "ui.switch_confirm") });
    expect(msg("ja", "ui.switch_confirm")).not.toBe(msg("ja", "ui.discard_confirm"));
    await expect(app.locator("#locale")).toHaveValue("ja");
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
    await expect(dataRows(app).locator(".cell.num")).toHaveText(["5", "6", "7"]);
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toHaveClass(/(^|\s)save-failed(\s|$)/);
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));
    expect(lines).toHaveLength(0);
    // 待ち受けは行ごとに断ったので、ファイルは起動したときのまま。
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });

  // 読み直しも同じロケールの行を描き直すので、抱えている訳を同じように捨てる。尋ね方が
  // 切り替えと違えば、片方の道だけ黙って捨てることになる。
  test("読み直す前に尋ね、断れば何も変えない", async ({ app, server }) => {
    const before = await server.readRoot(workingRel);
    await failRow(app, SAMPLE_LINES.goodbye, typed);
    const dialogs = watchDialogs(app, false);
    const lines = watchRequests(app, "/api/lines");

    await app.locator("#reload").click();
    await expect.poll(() => dialogs.length).toBe(1);
    expect(dialogs[0]).toEqual({ type: "confirm", message: msg("ja", "ui.discard_confirm") });
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toHaveClass(/(^|\s)save-failed(\s|$)/);
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));
    expect(lines).toHaveLength(0);
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });

  // 受けたときに捨てるのは、人が選んだ結果である（黙って破棄したのではない）。捨てたあとは
  // ファイルの中身を出し、保存の欄も「保存済み」に戻る。ファイルは1バイトも変わらない。
  test("尋ねて受けたときだけ、抱えていた訳を捨ててファイルの中身を並べ直す", async ({ app, server }) => {
    const before = await server.readRoot(workingRel);
    await failRow(app, SAMPLE_LINES.goodbye, typed);
    const dialogs = watchDialogs(app, true);

    await app.locator("#reload").click();
    await expect.poll(() => dialogs.length).toBe(1);
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(SAMPLE.goodbye.ja);
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).not.toHaveClass(/(^|\s)save-failed(\s|$)/);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });

  // 受けても読めなかったら、何も変わっていないのが正しい（load の注記）。捨てるのは読めた
  // ときだけで、読めないまま訳を消すと、ファイルにも画面にも訳が無くなる。
  test("尋ねて受けても読み直しに失敗したら、抱えていた訳を捨てない", async ({ app, server }) => {
    await failRow(app, SAMPLE_LINES.goodbye, typed);
    const dialogs = watchDialogs(app, true);
    await app.route(isPath("/api/lines"), (route) => route.fulfill({ status: 500, body: "boom" }));

    await app.locator("#reload").click();
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    expect(dialogs).toHaveLength(1);
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toHaveClass(/(^|\s)save-failed(\s|$)/);
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));
  });

  // 切り替えを受けて読んでいるあいだに、別のロケールを選んで断ったとき。断ったのだから
  // 何も変えない。読み込みは続くので、欄は読んでいる先へ戻す。以前は画面に出ている前の
  // ロケールへ戻したので、読み込みが返ると、欄は前のロケール、一覧は読んだロケールになった。
  test("切り替えを受けて読んでいるあいだに別のロケールを選んで断ると、欄は読んでいる先へ戻る", async ({
    app,
    server,
  }) => {
    await failRow(app, SAMPLE_LINES.goodbye, typed);
    const dialogs = answerDialogs(app, [true, false]);
    const lines = await holdLines(app);

    await app.locator("#locale").selectOption("he");
    await expect.poll(() => dialogs.length).toBe(1);
    await expect.poll(() => lines.asked).toEqual(["he"]);
    // he はまだ返らない。ja を選び直して、尋ねられたら断る。
    await app.locator("#locale").selectOption("ja");
    await expect.poll(() => dialogs.length).toBe(2);
    expect(dialogs[1]).toEqual({ type: "confirm", message: msg("ja", "ui.switch_confirm") });
    await expect(app.locator("#locale")).toHaveValue("he");

    lines.release("he");
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
    await expect(app.locator("#locale")).toHaveValue("he");
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_clean"));
    expect(lines.asked).toEqual(["he"]);
    // 1つ目は受けたので、打った訳は捨てた。ファイルに入れてはいない。
    expect(await server.readRootText(workingRel)).not.toContain(typed);
  });

  // 欄を開いたまま読み直しを押すと、欄から離れた時点で保存が走る（blur）。以前はその返事を
  // 待たずに尋ねたので、返事が先か後かで尋ねたり尋ねなかったりし、尋ねた文（「その訳は
  // 消えます」）と違って訳はファイルに入った。いまは送り終えてから決める（app.js の
  // askDiscard）。送れていれば捨てる訳は無いので、尋ねずにそのまま読み直す。
  test("打った直後に読み直しを押すと、送り終えてから決め、送れていれば尋ねずに読み直す", async ({ app, server }) => {
    const before = await server.readRootText(workingRel);
    const hold = gate();
    await app.route(isPath("/api/rows"), async (route) => {
      await hold.promise;
      await route.continue();
    });
    const dialogs = watchDialogs(app, false);
    const lines = watchRequests(app, "/api/lines");

    await typeTranslation(app, SAMPLE_LINES.goodbye, typed);
    await app.locator("#reload").click();
    // 保存が返るまでは、尋ねもせず、読みにもいかない。
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_saving"));
    expect(dialogs).toHaveLength(0);
    expect(lines).toHaveLength(0);

    hold.release();
    await expect.poll(() => lines.length).toBe(1);
    await waitForSaved(app);
    expect(dialogs).toHaveLength(0);
    const after = await server.readRootText(workingRel);
    expect(after).toBe(before.replace(`,${SAMPLE.goodbye.source},\n`, `,${SAMPLE.goodbye.source},${typed}\n`));
    // 読み直した一覧に、送った訳がファイルの値として出る。
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).not.toHaveClass(/(^|\s)unsaved(\s|$)/);
  });

  // 切り替えも同じ。打ってすぐ別のロケールを選んだとき、保存の返事を待たずに尋ねると、
  // 断れば選択が戻り、受ければ「消えます」と言った訳がファイルに入る。送り終えてから
  // 決めれば、送れた訳のために尋ねることはなく、選んだロケールへそのまま移る。
  test("打った直後にロケールを切り替えても、送り終えてから決め、送れていれば尋ねずに切り替える", async ({
    app,
    server,
  }) => {
    const before = await server.readRootText(workingRel);
    const hold = gate();
    await app.route(isPath("/api/rows"), async (route) => {
      await hold.promise;
      await route.continue();
    });
    const dialogs = watchDialogs(app, false);
    const lines = watchRequests(app, "/api/lines");

    await typeTranslation(app, SAMPLE_LINES.goodbye, typed);
    await editor(app).press("Escape");
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_saving"));
    await app.locator("#locale").selectOption("he");
    expect(dialogs).toHaveLength(0);
    expect(lines).toHaveLength(0);
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));

    hold.release();
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", heRel));
    await expect(app.locator("#locale")).toHaveValue("he");
    await waitForSaved(app);
    expect(dialogs).toHaveLength(0);
    expect(lines).toHaveLength(1);
    const after = await server.readRootText(workingRel);
    expect(after).toBe(before.replace(`,${SAMPLE.goodbye.source},\n`, `,${SAMPLE.goodbye.source},${typed}\n`));
  });

  // 送っても残る訳（ここでは待ち受けに届かない訳）があれば、毎回尋ねる。尋ねる前に、送れる
  // ものはもう1度送ってみる。受けたら、尋ねた文のとおりその訳は捨てる。読み込みが返るまでの
  // あいだに送り直しの時計が切れても送らない。以前はここで送り直しが走り、原因が消えていれば
  // 「消えます」と言って受けてもらった訳がファイルに入った。
  test("送り終えても残る訳があれば尋ね、受けたら読み込みのあいだに送り直しの時計が切れても送らない", async ({
    page,
    server,
  }) => {
    await countRowPosts(page);
    await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
    await openApp(page, server);
    await page.clock.pauseAt(new Date("2026-01-01T01:00:00Z"));
    const before = await server.readRoot(workingRel);

    let down = true;
    await page.route(isPath("/api/rows"), (route) =>
      down
        ? route.fulfill({
            status: 503,
            contentType: "application/json",
            body: JSON.stringify({ message: msg("ja", "error.save_failed") }),
          })
        : route.continue(),
    );
    await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
    await editor(page).press("Escape");
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
    expect(await rowPosts(page)).toBe(1);

    const hold = gate();
    await page.route(isPath("/api/lines"), async (route) => {
      await hold.promise;
      await route.continue();
    });
    const dialogs = watchDialogs(page, true);
    await page.locator("#reload").click();
    await expect.poll(() => dialogs.length).toBe(1);
    expect(dialogs[0]).toEqual({ type: "confirm", message: msg("ja", "ui.discard_confirm") });
    // 尋ねる前に、もう1度送ってみた（届かなかったので残った）。
    expect(await rowPosts(page)).toBe(2);

    // 受けた。読み込みはまだ返らない。そのあいだに原因が消え、送り直しの時計も切れる。
    down = false;
    await page.clock.runFor(60_000);
    expect(await rowPosts(page)).toBe(2);

    hold.release();
    await expect(translationCell(page, SAMPLE_LINES.goodbye)).toHaveText(SAMPLE.goodbye.ja);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_clean"));
    await page.clock.runFor(60_000);
    expect(await rowPosts(page)).toBe(2);
    expect((await server.readRoot(workingRel)).equals(before), "捨てると答えた訳がファイルに入った").toBe(true);
  });
});

test.describe("保存を送り直しているとき", () => {
  // 送り直しは途中で諦めない（app.js の retryDelays、doc.go「訳を失わない」）。読み直しに
  // 失敗したら何も変わっていないのが正しい（load の注記）。以前は load が送り直しの時計と
  // saveError を読む前に片付け、読めなかったときに戻さなかった。訳は「未保存 1 件」と出たまま
  // どこへも送られず、原因（ゲームがファイルを開いている）が消えてもファイルに入らなかった。
  test("読み直しを受けたが読めなかったとき、送り直しを続けて訳をファイルへ入れる", async ({ page, server }) => {
    await page.clock.install();
    await openApp(page, server);

    // 待ち受けが書けない状態（Windows の共有違反で返る 503 と同じ形）。
    const rows = isPath("/api/rows");
    await page.route(rows, (route) =>
      route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ message: msg("ja", "error.save_failed") }),
      }),
    );
    await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
    await editor(page).press("Escape");
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    const dialogs = watchDialogs(page, true);
    await page.route(isPath("/api/lines"), (route) => route.fulfill({ status: 500, body: "boom" }), { times: 1 });
    await page.locator("#reload").click();
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    expect(dialogs).toHaveLength(1);
    await expect(translationCell(page, SAMPLE_LINES.goodbye)).toHaveText(typed);
    // 読めなかったので何も変わっていない。保存できていないことも出たまま。
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    // 原因が消えた。送り直しの間隔の最大（30 秒）を超えるぶん時計を進める。
    await page.unroute(rows);
    await page.clock.runFor(60_000);
    await expect
      .poll(async () => (await server.readRootText(workingRel)).includes(`,${typed}\n`), { timeout: 5_000 })
      .toBe(true);
    await waitForSaved(page);
  });

  // 捨てると答えたあとは、読み込みのあいだに送り直しの時計が切れても送らない（app.js の
  // flush）。そのとき時計は送らずに消える。読めなかったら、捨てなかったのだから送り直しへ
  // 戻らなければならない。戻らないと、原因が消えてもその訳は二度と送られない
  // （retryDelays の「諦めない」、app.js の stopHolding）。上の試験と違い、時計を止めて、
  // 読み込みのあいだに送り直しの時計を確かに切らせてから、読めなかったことにする。
  test("捨てると答えたあと読めなかったときも、読み込みのあいだに切れた送り直しを引き直して訳をファイルへ入れる", async ({
    page,
    server,
  }) => {
    await countRowPosts(page);
    await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
    await openApp(page, server);
    await page.clock.pauseAt(new Date("2026-01-01T01:00:00Z"));

    const rows = isPath("/api/rows");
    await page.route(rows, (route) =>
      route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ message: msg("ja", "error.save_failed") }),
      }),
    );
    await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
    await editor(page).press("Escape");
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    const hold = gate();
    await page.route(isPath("/api/lines"), async (route) => {
      await hold.promise;
      await route.fulfill({ status: 500, body: "boom" });
    });
    const dialogs = watchDialogs(page, true);
    await page.locator("#reload").click();
    await expect.poll(() => dialogs.length).toBe(1);
    const asked = await rowPosts(page);

    // 読み込みのあいだに送り直しの時計を切らせる。捨てると答えたので送らない。
    await page.clock.runFor(60_000);
    expect(await rowPosts(page)).toBe(asked);

    // 読めなかった。何も捨てていないので、訳は残り、保存できていないことも出たまま。
    hold.release();
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.load_failed"));
    await expect(translationCell(page, SAMPLE_LINES.goodbye)).toHaveText(typed);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    // 原因が消えれば、引き直した送り直しで訳がファイルに入る。
    await page.unroute(rows);
    await page.clock.runFor(60_000);
    await expect
      .poll(async () => (await server.readRootText(workingRel)).includes(`,${typed}\n`), { timeout: 5_000 })
      .toBe(true);
    await waitForSaved(page);
    expect(await rowPosts(page)).toBeGreaterThan(asked);
  });

  // 捨てると答えた切り替えを、読み終える前に別の切り替えで上書きしたとき。前の読み込みは
  // 描かない（app.js の load）が、捨てると答えた印の後始末はする。ただし印は、あとの切り替えで
  // 捨てると答え直したほうのものなので、前の応答が倒してはならない。倒すと、あとの読み込みが
  // 返るまでのあいだに送り直しの時計が切れ、捨てると答えた訳がファイルに入る。
  test("捨てると答えた切り替えを読み終える前に選び直すと、前の応答が返っても捨てると答えた訳を送らない", async ({
    page,
    server,
  }) => {
    await countRowPosts(page);
    await watchLinesSettled(page);
    await page.clock.install({ time: new Date("2026-01-01T00:00:00Z") });
    await openApp(page, server);
    await page.clock.pauseAt(new Date("2026-01-01T01:00:00Z"));
    const before = await server.readRoot(workingRel);

    let down = true;
    await page.route(isPath("/api/rows"), (route) =>
      down
        ? route.fulfill({
            status: 503,
            contentType: "application/json",
            body: JSON.stringify({ message: msg("ja", "error.save_failed") }),
          })
        : route.continue(),
    );
    await typeTranslation(page, SAMPLE_LINES.goodbye, typed);
    await editor(page).press("Escape");
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    const lines = await holdLines(page);
    const dialogs = watchDialogs(page, true);
    await page.locator("#locale").selectOption("he");
    await page.clock.runFor(localeDelay);
    await expect.poll(() => dialogs.length).toBe(1);
    await expect.poll(() => lines.asked).toEqual(["he"]);
    await page.locator("#locale").selectOption("ja");
    await page.clock.runFor(localeDelay);
    await expect.poll(() => dialogs.length).toBe(2);
    await expect.poll(() => lines.asked).toEqual(["he", "ja"]);
    const asked = await rowPosts(page);

    // 前の読み込み（he）が先に返る。描かない。
    lines.release("he");
    await expect.poll(() => linesSettled(page)).toEqual(["ja", "he"]);
    await expect(page.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
    // 原因が消える。あとの読み込みはまだ返らないので、保存へ回る道を通っても送らない。
    // 送り直しの時計は、2つ目を尋ねる前の送り直し（settle）が止めているので、ここでは
    // 書き出しを押して保存へ回す（書き出しは先に flush を呼ぶ）。読み込みのあいだは
    // 入力欄を開かない（app.js の load）ので、行を開いて閉じる道は使えない。
    //
    // 「送らない」ことは POST の数だけで確かめる。書き出しは押したその場で flush を呼ぶので、
    // 送るなら押した直後に数が増えている。書き出しの結果の文言は確かめない。結果の欄が
    // 空でなくなるのを待つのは、押した書き出しが画面に届いた（保存へ回った）ことの確かめに
    // 使うだけにする（書き出しは始めに欄を空にし、flush を待ってから結果を出す）。
    down = false;
    await translationCell(page, SAMPLE_LINES.goodbye).click();
    await expect(editor(page)).toHaveCount(0);
    await page.locator("#export-open").click();
    await page.locator("#export-working").click();
    expect(await rowPosts(page)).toBe(asked);
    await expect(page.locator("#export-state")).not.toBeEmpty();
    await page.clock.runFor(60_000);
    expect(await rowPosts(page)).toBe(asked);

    lines.release("ja");
    await expect(translationCell(page, SAMPLE_LINES.goodbye)).toHaveText(SAMPLE.goodbye.ja);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_clean"));
    await expect(page.locator("#locale")).toHaveValue("ja");
    await page.clock.runFor(60_000);
    expect(await rowPosts(page)).toBe(asked);
    expect((await server.readRoot(workingRel)).equals(before), "捨てると答えた訳がファイルに入った").toBe(true);
  });
});

test.describe("送り終えるのを待っているあいだに、もう一度押したとき", () => {
  // openWithClock は偽の時計を入れてから画面を開く。時計は止めないので、ふだんどおり進む。
  // ロケールの欄で選んだあとに page.clock.runFor(localeDelay) を呼ぶと、読みにいくまでの待ち
  // （app.js の localeDelay）をその場で終えられる。切り替えが送り終えるのを待っている状態を、
  // 実時間の待ちに頼らずに作るためにある。
  async function openWithClock(page, server) {
    await page.clock.install();
    await openApp(page, server);
    return page;
  }

  // 読み直し（切り替え）は、送っている保存が返るまで決めない（askDiscard）。そのあいだに
  // もう一度押すと、前に押したほうは何もせず、あとで押したほうに任せる。任せないと、
  // 返ったところで2回読み、残る訳があれば2回尋ねる。
  test("読み直しを2度押しても、読むのも尋ねるのも1回だけ", async ({ app }) => {
    const hold = gate();
    await app.route(isPath("/api/rows"), async (route) => {
      await hold.promise;
      await route.continue();
    });
    const dialogs = watchDialogs(app, false);
    const lines = watchRequests(app, "/api/lines");

    await typeTranslation(app, SAMPLE_LINES.goodbye, typed);
    await app.locator("#reload").click();
    await app.locator("#reload").click();
    hold.release();

    await expect.poll(() => lines.length).toBe(1);
    await waitForSaved(app);
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    expect(lines).toHaveLength(1);
    expect(dialogs).toHaveLength(0);
  });

  // 2度押すと、2つの確認（askDiscard）が同じ送り終わりを待つ。待つあいだに別の行へ打ち足した訳は、
  // 送っている最中なのでまだ送られない。返ったところで先に押したほうがそれを送り始めると、以前は
  // あとで押したほうがその返事を待たずに尋ねた（送っている最中の flush はすぐ戻るため。app.js の
  // settle）。受ければ読み込みが走り、「その訳は消えます」と尋ねた訳が、そのあと返った保存で
  // ファイルに入った。その返事も待ってから決める。送れていれば尋ねずに読み直す。
  test("待つあいだに打ち足した訳を先に押したほうが送り始めても、その返事を待ってから決める", async ({ app, server }) => {
    const before = await server.readRootText(workingRel);
    const added = "もしもし。";
    const first = gate();
    const second = gate();
    const sent = [];
    await app.route(isPath("/api/rows"), async (route) => {
      sent.push(route.request().postDataJSON().edits.map((edit) => edit.id));
      await (sent.length === 1 ? first.promise : second.promise);
      await route.continue();
    });
    const dialogs = watchDialogs(app, true);
    const lines = watchRequests(app, "/api/lines");

    await typeTranslation(app, SAMPLE_LINES.goodbye, typed);
    await editor(app).press("Escape");
    await expect.poll(() => sent.length).toBe(1);
    await typeTranslation(app, SAMPLE_LINES.hello, added);
    await editor(app).press("Escape");
    await app.locator("#reload").click();
    await app.locator("#reload").click();
    first.release();

    // 打ち足したぶんを送り始めた。その返事が返るまでは、尋ねもせず、読みにもいかない。
    // 確認が開いていれば、閉じるまで evaluate は返らない（尋ねていれば dialogs に入っている）。
    await expect.poll(() => sent).toEqual([[SAMPLE_LINES.goodbye], [SAMPLE_LINES.hello]]);
    await app.evaluate(() => true);
    expect(dialogs).toHaveLength(0);
    expect(lines).toHaveLength(0);

    second.release();
    await expect.poll(() => lines.length).toBe(1);
    await waitForSaved(app);
    expect(dialogs).toHaveLength(0);
    expect(sent).toHaveLength(2);
    expect(await server.readRootText(workingRel)).toBe(
      before
        .replace(`,${SAMPLE.goodbye.source},\n`, `,${SAMPLE.goodbye.source},${typed}\n`)
        .replace(`,${SAMPLE.hello.source},${SAMPLE.hello.ja}\n`, `,${SAMPLE.hello.source},${added}\n`),
    );
    // 読み直した一覧に、送った2つの訳がファイルの値として出る。
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    await expect(translationCell(app, SAMPLE_LINES.hello)).toHaveText(added);
  });

  // 切り替えでも同じ。he を選んだあとで ja へ戻したら、読むのは戻した ja だけで、
  // he は1度も読まない。
  test("返る前にロケールを選び直したら、あとで選んだほうだけを読む", async ({ page, server }) => {
    const app = await openWithClock(page, server);
    const hold = gate();
    await app.route(isPath("/api/rows"), async (route) => {
      await hold.promise;
      await route.continue();
    });
    const lines = [];
    app.on("request", (req) => {
      const url = new URL(req.url());
      if (url.pathname === "/api/lines") {
        lines.push(url.searchParams.get("locale"));
      }
    });

    await typeTranslation(app, SAMPLE_LINES.goodbye, typed);
    await editor(app).press("Escape");
    await app.locator("#locale").selectOption("he");
    // he への切り替えが、送り終えるのを待つところまで進める。
    await app.clock.runFor(localeDelay);
    await app.locator("#locale").selectOption("ja");
    await app.clock.runFor(localeDelay);
    hold.release();

    await expect.poll(() => lines).toEqual(["ja"]);
    await waitForSaved(app);
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    expect(lines).toEqual(["ja"]);
  });

  // 切り替えを選んで返事を待っているあいだに読み直しを押すと、切り替えは読み直しに任せて
  // 何もしない（askDiscard が stale を返す）。以前は読み直しが欄の値（切り替え先）を読んだ。
  // 送れたときは、切り替え先を前のロケールの条件と検索語を付けたまま読んだ（切り替えなら
  // 外す。「ロケールの切り替え」の試験）。読み直すのは画面に出ているロケールで、欄も押した
  // 時点でそこへ戻す（app.js の読み直し）。
  test("切り替えの返事を待つあいだに読み直しを押すと、画面に出ているロケールを条件と検索語を付けたまま読み直す", async ({
    page,
    server,
  }) => {
    const app = await openWithClock(page, server);
    const hold = gate();
    await app.route(
      isPath("/api/rows"),
      async (route) => {
        await hold.promise;
        await route.continue();
      },
      { times: 1 },
    );
    const dialogs = watchDialogs(app, false);
    const lines = [];
    app.on("request", (req) => {
      const url = new URL(req.url());
      if (url.pathname === "/api/lines") {
        lines.push(url.searchParams.get("locale"));
      }
    });

    // 原文で絞る。he のファイルに Goodbye. の行は無いので、持ち越せば he の一覧は 0 行になる。
    await app.locator("#search").fill(SAMPLE.goodbye.source);
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));
    await typeTranslation(app, SAMPLE_LINES.goodbye, typed);
    await editor(app).press("Escape");
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_saving"));
    await app.locator("#locale").selectOption("he");
    // he への切り替えが、送り終えるのを待つところまで進める。
    await app.clock.runFor(localeDelay);
    await app.locator("#reload").click();
    // 押した時点で切り替えは取りやめになり、欄も画面に出ているロケールへ戻る。
    await expect(app.locator("#locale")).toHaveValue("ja");
    hold.release();

    await expect.poll(() => lines).toEqual(["ja"]);
    await waitForSaved(app);
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
    await expect(app.locator("#locale")).toHaveValue("ja");
    await expect(app.locator("#search")).toHaveValue(SAMPLE.goodbye.source);
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    expect(lines).toEqual(["ja"]);
    expect(dialogs).toHaveLength(0);
  });

  // 送れずに訳が残ったときは、読み直しを尋ねる。以前は断ると、欄だけが切り替え先を指した
  // まま残り、一覧・ファイルの名前・送り直しの宛先は前のロケールのままだった。そこで欄から
  // 前のロケールを選び直すと「切り替えると消えます」と尋ねられ、受けると、画面に出ている
  // ロケールのまま、送り直している訳を捨てた。断ったのだから、欄も画面に出ているロケールを指す。
  test("切り替えの返事を待つあいだに読み直しを押し、送れずに読み直しを断っても、欄は画面に出ているロケールを指す", async ({
    page,
    server,
  }) => {
    const app = await openWithClock(page, server);
    const before = await server.readRoot(workingRel);
    const hold = gate();
    let first = true;
    const posted = [];
    await app.route(isPath("/api/rows"), async (route) => {
      posted.push(JSON.parse(route.request().postData() ?? "{}").locale);
      if (first) {
        first = false;
        await hold.promise;
      }
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ message: msg("ja", "error.save_failed") }),
      });
    });
    const dialogs = watchDialogs(app, false);
    const lines = watchRequests(app, "/api/lines");

    await typeTranslation(app, SAMPLE_LINES.goodbye, typed);
    await editor(app).press("Escape");
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_saving"));
    await app.locator("#locale").selectOption("he");
    // he への切り替えが、送り終えるのを待つところまで進める。
    await app.clock.runFor(localeDelay);
    await app.locator("#reload").click();
    hold.release();

    await expect.poll(() => dialogs.length).toBe(1);
    expect(dialogs[0]).toEqual({ type: "confirm", message: msg("ja", "ui.discard_confirm") });
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_retrying"));
    await expect(app.locator("#locale")).toHaveValue("ja");
    await expect(app.locator("#file-path")).toHaveText(fileLabel("ja", workingRel));
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText(typed);
    expect(lines).toHaveLength(0);
    expect(dialogs).toHaveLength(1);
    expect(posted.every((locale) => locale === "ja")).toBe(true);
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });
});
