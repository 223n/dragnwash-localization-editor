// 保存が失敗したときに、訳を失わず、諦めずに送り直すことを見る。
//
// app.js の冒頭の約束のうち、いちばん重いのは「訳を失わない」である。保存が失敗する
// 道は2つあり、画面はそれぞれ別の形で訳を抱える（app.js の state.pending と state.failed）。
//
//   要求そのものが落ちる  届かない、503（ゲームがファイルを開いていて書けない）など。
//                         訳は未保存のまま抱え、間隔を広げながら送り直し続ける。
//                         待っても直らない 400・404・415 だけは送り直さず、案内する。
//                         届かないときと、この3つのときは、まだファイルに入っていない
//                         訳を帯に並べる（app.js の renderUnsent）。
//   行ごとに断られる      待ち受けがその行を書かなかった（行がずれた、など）。
//                         訳は「保存できない行」として値ごと残し、自動保存の対象からだけ外す。
//
// どちらも、ファイルに入ったかどうかは画面ではなくディスクのバイトで確かめる。
// 画面が「保存済み」と言っても、ファイルに入っていなければ訳は失われている。
//
// 失敗は page.route で作る。要求を落とす（abort）、応答を差し替える（fulfill）、
// 本文のキーを書き換えて本物の待ち受けに断らせる（continue）の3通りを使い分ける。
// 本物の 503 は、作業コピーを書けない状態にして起こす。
//
// 送り直しの時計は page.clock で止めて進める。実時間で待つと、遅い機械では
// 送り直しが試験の手より先に走り、見たい順番が崩れる。
import { randomUUID } from "node:crypto";
import { chmod, readFile, rename, rm, writeFile } from "node:fs/promises";
import { basename, dirname, join } from "node:path";

import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import {
  HEADER,
  SAMPLE,
  SAMPLE_LINES,
  field,
  keyFor,
  sampleRepo,
  sampleWorkingCopy,
  workingCopy,
} from "../support/repo.mjs";
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

// app.js の retryDelays と同じ並び。送り直しの間隔を変えたら、ここも合わせる。
// 見ているのは値そのものより「広げていく」「最後の間隔のまま続ける」の2つである。
const retryDelays = [500, 1_000, 2_000, 5_000, 15_000, 30_000];

// 自動保存の待ち（internal/web の api.go の autosaveDelay。/api/bootstrap で画面へ渡る）。
const autosaveDelay = 1_500;

// ロケールの欄で選んでから読みにいくまでの待ち（app.js の localeDelay）。
const localeDelay = 400;

// 帯（#save-state）と行の class を見る。saving は送っている最中、failed は帯の
// 「保存できない」側、unsaved と save-failed は行の印（app.js の markRow）。
const SAVING = /(^|\s)saving(\s|$)/;
const FAILED = /(^|\s)failed(\s|$)/;
const UNSAVED = /(^|\s)unsaved(\s|$)/;
const SAVE_FAILED = /(^|\s)save-failed(\s|$)/;

// 見本はわざと BOM 付きで、行ごとに LF と CRLF を混ぜる（autosave.spec.mjs と同じ）。
// 失敗のあとで送り直した保存が、触っていない行を1バイトも動かさないことまで見るため。
// 添字 0 がヘッダーなので、偶数の物理行（6行目の goodbye など）が CRLF になる。
const mixedEol = (i) => (i % 2 === 1 ? "\r\n" : "\n");

test.use({
  repo: sampleRepo({ workingCopy: sampleWorkingCopy({ bom: true, eol: mixedEol }) }),
});

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

// withTranslation は作業コピーの1行（改行つき）の最終フィールドを value に差し替えた形。
// 見本の訳にはカンマも引用符も無いので、最後のカンマから行末までを差し替えれば足りる。
function withTranslation(line, value) {
  return line.toString("utf8").replace(/,[^,\r\n]*(\r?\n)?$/, (whole, eol) => `,${field(value)}${eol ?? ""}`);
}

// expectOnlyChanged は after が before と比べて changed の行だけ変わったことを確かめる。
// changed は 物理行番号 → その行の期待する中身（改行つき）。
function expectOnlyChanged(before, after, changed = {}) {
  const was = splitLines(before);
  const now = splitLines(after);
  expect(now).toHaveLength(was.length);
  for (let i = 0; i < was.length; i++) {
    const n = i + 1;
    if (Object.hasOwn(changed, n)) {
      expect(now[i].toString("utf8"), `${n}行目`).toBe(changed[n]);
      continue;
    }
    expect(now[i].equals(was[i]), `${n}行目が変わった`).toBe(true);
  }
}

// openPaused は時計を差し替えてから画面を開き、開き終えたところで時計を止める。
// 止めたあとは page.clock.runFor で進めた分だけ、自動保存と送り直しの時計が進む。
async function openPaused(page, server) {
  await page.clock.install();
  await openApp(page, server);
  const now = await page.evaluate(() => Date.now());
  await page.clock.pauseAt(now + 1_000);
}

// unsent は、まだファイルに入っていない訳の一覧（帯の中の #unsent）。待ち受けに届かない
// ときと、待ち受けが保存を受け付けないときにだけ出る（app.js の renderUnsent）。
function unsent(page) {
  return page.locator("#unsent");
}

// expectUnsent は、一覧に行 n の訳 value が、行番号とキーを添えて1件だけ出ていることを確かめる。
// 新しい画面では、検索の欄にキーを打てばその行が出る。
async function expectUnsent(page, n, key, value) {
  await expect(unsent(page)).toBeVisible();
  await expect(page.locator("#unsent-title")).toHaveText(msg("ja", "ui.unsent_title"));
  const items = page.locator("#unsent-list > li");
  await expect(items).toHaveCount(1);
  await expect(items.first()).toHaveText(`${msg("ja", "ui.unsent_line", { line: n })} ${key}: ${value}`);
}

// closeEditor は Escape で入力欄を閉じる。閉じると、待たずに保存へ回る（commitEditor）。
async function closeEditor(page) {
  await editor(page).press("Escape");
  await expect(editor(page)).toHaveCount(0);
}

// expectNoNewPost は、送り直しの時計が切れていない（新しい要求を送っていない）ことを確かめる。
//
// 時計が切れていれば、flush がその場で帯を「保存しています」にしてから送る。先に帯が
// 送っている最中でないことを待ち、そのあとで数を読む。時計が切れていたなら、帯が
// 戻るまでに route の手が呼ばれているので、数が増えている。
async function expectNoNewPost(page, posts, count) {
  await expect(saveState(page)).not.toHaveClass(SAVING);
  expect(posts).toHaveLength(count);
}

// misplace は本文の中の line 行のキーを別のものに書き換える。待ち受けは
// 「同じ行番号にいま別のキーの行がある」と見て、その行を書かずに断る（error.row_moved）。
function misplace(route, line) {
  const body = route.request().postDataJSON();
  for (const edit of body.edits) {
    if (edit.line === line) {
      edit.key = keyFor("この行はここに無い");
    }
  }
  return route.continue({ postData: JSON.stringify(body) });
}

// rowMovedNote は行に添える「この行は保存できません: この行はずれています…」の文。
function rowMovedNote() {
  return msg("ja", "ui.row_error", { reason: msg("ja", "error.row_moved") });
}

// makeUnwritable は rel を、待ち受けの保存（同じディレクトリに一時ファイルを作って
// rename で置き換える、internal/publish の WriteBytes）が通らない状態にする。
//
// 閉じる相手は Go の試験（internal/web の makeReadOnly）と同じ理由で OS ごとに違う。
//   - Windows … ファイルの読み取り専用の属性で、置き換えが拒まれる
//   - POSIX   … rename の可否はディレクトリの書き込み権で決まるので、ディレクトリを閉じる
// 実際に書けなくなったかを同じ手順で確かめ、root で走っているなどで効かなければ飛ばす。
//
// 戻す関数を返す。何度呼んでもよい。戻す処理は server の後始末にも頼んでおく。試験が
// 戻す前で止まったまま時間切れになると、Playwright は試験の finally より先に server を
// 畳むので、finally で戻すだけでは閉じたままのディレクトリを消しにいき、EACCES で見本が残る。
async function makeUnwritable(server, rel) {
  const file = server.rootPath(rel);
  const windows = process.platform === "win32";
  const target = windows ? file : dirname(file);
  const [closed, open] = windows ? [0o444, 0o644] : [0o555, 0o755];
  await chmod(target, closed);
  let restored = false;
  const restore = async () => {
    if (restored) {
      return;
    }
    restored = true;
    await chmod(target, open);
  };
  server.beforeRemove(restore);

  const body = await readFile(file);
  const probe = join(dirname(file), `${basename(file)}.probe-${randomUUID()}`);
  let writable = true;
  try {
    await writeFile(probe, body);
    await rename(probe, file);
  } catch {
    writable = false;
  }
  await rm(probe, { force: true }).catch(() => {});
  if (writable) {
    await restore();
    test.skip(true, `${target} を書けない状態にできない（root で走っていると効かない）`);
  }
  return restore;
}

test.describe("要求そのものが落ちたとき", () => {
  test("届かなかった保存は未保存のまま抱え、失敗を画面に出し、戻れば送り直してファイルに入る", async ({
    page,
    server,
  }) => {
    // 届かなかったことを黙っていると、翻訳者は「保存済み」を信じて閉じにいく。
    // 抱えている訳を捨てると、原因が消えたあとに送るものが無い。
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    const posts = [];
    let down = true;
    await page.route("**/api/rows", (route) => {
      posts.push(route.request().postDataJSON());
      return down ? route.abort("connectionrefused") : route.continue();
    });

    const typed = "さようなら。";
    const n = SAMPLE_LINES.goodbye;
    await typeTranslation(page, n, typed);
    await closeEditor(page);

    // 常に見えている帯で「保存できない（試し続けている）」と言う。「未保存 1 件」と
    // 出すと、打ったばかりでまだ送っていない状態と見分けが付かない。
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
    await expect(saveState(page)).toHaveClass(FAILED);
    // 届かないこと（待ち受けが終わっているかもしれないこと）を、503 などの「届いたが
    // 書けなかった」と分けて言う。以前は「少し置いてから自動でもう一度送ります」と言い
    // 続けたが、待ち受けが終わっていれば、待っても送られない（起動し直すと URL が変わる）。
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.unreachable"));
    await expectUnsent(page, n, keyFor(SAMPLE.goodbye.source), typed);
    // 行は未保存の印のまま、打った訳を出し続ける。
    await expect(rowByLine(page, n)).toHaveClass(UNSAVED);
    await expect(translationCell(page, n)).toHaveText(typed);
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);

    down = false;
    await page.clock.runFor(retryDelays[0]);
    await expect.poll(async () => (await server.readRoot(workingRel)).equals(before), { timeout: 10_000 }).toBe(false);
    await waitForSaved(page);
    expect(posts).toHaveLength(2);
    expect(posts[1].edits).toEqual([expect.objectContaining({ line: n, translation: typed })]);
    expectOnlyChanged(before, await server.readRoot(workingRel), {
      [n]: withTranslation(splitLines(before)[n - 1], typed),
    });
    // 送れたら失敗の文とまだ入っていない訳の一覧は消える。残すと、直ったのにまだ壊れて
    // いるように見える。
    await expect(page.locator("#message")).toHaveText("");
    await expect(unsent(page)).toBeHidden();
    await expect(rowByLine(page, n)).not.toHaveClass(UNSAVED);
  });

  test("送り直しは間隔を広げながら続け、決めた回数を使い切っても諦めない", async ({ page, server }) => {
    // 諦めると、原因（ゲームがファイルを開いている）が消えたあとも、その訳は二度と
    // 送られない（app.js の retryDelays の注記。実際に起きた）。広げずに送り続けると、
    // 落ち続けているあいだ待ち受けを叩き続ける。
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    const posts = [];
    let down = true;
    await page.route("**/api/rows", (route) => {
      posts.push(route.request().postDataJSON());
      return down ? route.abort("connectionrefused") : route.continue();
    });

    const typed = "さようなら。";
    const n = SAMPLE_LINES.goodbye;
    await typeTranslation(page, n, typed);
    await closeEditor(page);
    await expect.poll(() => posts.length).toBe(1);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    // 決めた6つの間隔のあとも、最後の間隔（30秒）のまま2回続くことを見る。
    const waits = [...retryDelays, 30_000, 30_000];
    for (const [i, wait] of waits.entries()) {
      await page.clock.runFor(wait - 1);
      await expectNoNewPost(page, posts, i + 1);
      await page.clock.runFor(1);
      await expect.poll(() => posts.length).toBe(i + 2);
      // 落ちた応答を受け止め終えるまで待つ。受け止める前に時計を進めると、次の
      // 送り直しの時計がまだ無い。
      await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
      // 送り直すのは抱えている訳そのもの。
      expect(posts.at(-1).edits).toEqual([expect.objectContaining({ line: n, translation: typed })]);
    }
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);

    // 原因が消えたら、次の送り直しで入る。
    down = false;
    await page.clock.runFor(30_000);
    await waitForSaved(page);
    expectOnlyChanged(before, await server.readRoot(workingRel), {
      [n]: withTranslation(splitLines(before)[n - 1], typed),
    });
  });

  test("打ち直すと、送り直しの間隔が最初に戻る", async ({ page, server }) => {
    // 広がったままだと、翻訳者が直したのに最長30秒待たされる（app.js の onInput の注記）。
    await openPaused(page, server);
    const posts = [];
    await page.route("**/api/rows", (route) => {
      posts.push(route.request().postDataJSON());
      return route.abort("connectionrefused");
    });

    const n = SAMPLE_LINES.goodbye;
    await typeTranslation(page, n, "さようなら");
    await closeEditor(page);
    await expect.poll(() => posts.length).toBe(1);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
    // 2回送り直させる。次の間隔は 2000ms まで広がっている。
    for (const [i, wait] of retryDelays.slice(0, 2).entries()) {
      await page.clock.runFor(wait);
      await expect.poll(() => posts.length).toBe(i + 2);
      await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
    }

    // 打ち直して閉じる。閉じたときの保存も落ちる。
    await typeTranslation(page, n, "さようなら。");
    await closeEditor(page);
    await expect.poll(() => posts.length).toBe(4);
    expect(posts[3].edits).toEqual([expect.objectContaining({ line: n, translation: "さようなら。" })]);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    // 次の送り直しは最初の間隔で来る。
    await page.clock.runFor(retryDelays[0] - 1);
    await expectNoNewPost(page, posts, 4);
    await page.clock.runFor(1);
    await expect.poll(() => posts.length).toBe(5);
  });

  test("200 でない応答が saved: true と書いていても、未保存の控えを捨てない", async ({ page, server }) => {
    // 200 でない応答の saved を信じると、ファイルに入っていない値を「保存できた」と読み、
    // 未保存の控えを捨てる。訳はファイルにも画面の控えにも残らない（app.js の onSaved の注記）。
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    const typed = "さようなら。";
    const n = SAMPLE_LINES.goodbye;
    let lie = true;
    await page.route("**/api/rows", (route) => {
      if (!lie) {
        return route.continue();
      }
      return route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({
          message: msg("ja", "error.save_failed"),
          results: [{ line: n, saved: true, translation: typed, badges: [] }],
        }),
      });
    });

    await typeTranslation(page, n, typed);
    await closeEditor(page);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
    // 待ち受けの文面（本文の message）をそのまま出す。
    await expect(page.locator("#message")).toHaveText(msg("ja", "error.save_failed"));
    await expect(rowByLine(page, n)).toHaveClass(UNSAVED);
    await expect(rowByLine(page, n)).not.toHaveClass(SAVE_FAILED);
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);

    // 控えを持っていたので、次の送り直しで本当に入る。
    lie = false;
    await page.clock.runFor(retryDelays[0]);
    await waitForSaved(page);
    expectOnlyChanged(before, await server.readRoot(workingRel), {
      [n]: withTranslation(splitLines(before)[n - 1], typed),
    });
  });

  test("送っている間に打った訳は、その保存が落ちたら送り直しで一緒に送る", async ({ page, server }) => {
    // 返ってくるまでのあいだに打った訳は、自動保存の時計を待っている。先の保存が
    // 落ちたときに送り直しの時計と別々に扱うと、どちらかの訳が次の打鍵まで取り残される。
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    const posts = [];
    let release;
    const held = new Promise((resolve) => {
      release = resolve;
    });
    await page.route("**/api/rows", async (route) => {
      posts.push(route.request().postDataJSON());
      if (posts.length === 1) {
        await held;
        return route.abort("connectionrefused");
      }
      return route.continue();
    });

    const goodbye = SAMPLE_LINES.goodbye;
    const wonderful = SAMPLE_LINES.wonderful;
    await typeTranslation(page, goodbye, "さようなら。");
    await closeEditor(page);
    await expect.poll(() => posts.length).toBe(1);
    // 1つ目が返る前に、別の行を打つ（閉じない。自動保存の時計を待つ状態）。
    await typeTranslation(page, wonderful, "すごい！");
    release();
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    // 最初の送り直しで、2行とも送る。
    await page.clock.runFor(retryDelays[0]);
    await expect.poll(() => posts.length).toBe(2);
    expect(posts[1].edits.map((edit) => edit.line)).toEqual([goodbye, wonderful]);
    await waitForSaved(page);
    const was = splitLines(before);
    expectOnlyChanged(before, await server.readRoot(workingRel), {
      [goodbye]: withTranslation(was[goodbye - 1], "さようなら。"),
      [wonderful]: withTranslation(was[wonderful - 1], "すごい！"),
    });
  });

  test("作業コピーが読み取り専用の形に変わった 422 でも、未保存を抱えて送り直し、戻ればファイルに入る", async ({
    page,
    server,
  }) => {
    // 待ち受けはヘッダーを受理できないファイルを丸ごと読み取り専用として断る（422、
    // 行ごとの結果は無い）。よそが作業コピーを書き直している途中にも起きる。行ごとの
    // 理由が無い失敗を「保存できた」とも「保存できない行」とも読まず、未保存のまま抱える。
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    const renamed = HEADER.working.replace("source_en", "source");
    const broken = Buffer.from(before.toString("utf8").replace(HEADER.working, renamed), "utf8");
    expect(broken.equals(before)).toBe(false);
    await server.writeRoot(workingRel, broken);

    const typed = "さようなら。";
    const n = SAMPLE_LINES.goodbye;
    await typeTranslation(page, n, typed);
    const refused = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/rows");
    await closeEditor(page);
    expect((await refused).status()).toBe(422);

    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
    await expect(page.locator("#message")).toHaveText(msg("ja", "error.file_readonly"));
    await expect(rowByLine(page, n)).toHaveClass(UNSAVED);
    await expect(rowByLine(page, n)).not.toHaveClass(SAVE_FAILED);
    expect((await server.readRoot(workingRel)).equals(broken)).toBe(true);

    // 元の中身に戻ると、画面が読んだときの版と一致するので、送り直しで入る。
    await server.writeRoot(workingRel, before);
    const accepted = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/rows");
    await page.clock.runFor(retryDelays[0]);
    expect((await accepted).status()).toBe(200);
    await waitForSaved(page);
    expectOnlyChanged(before, await server.readRoot(workingRel), {
      [n]: withTranslation(splitLines(before)[n - 1], typed),
    });
  });

  test("Cookie を失って 404 の平文が返ったら、送り直しを止め、訳を抱えたまま案内し、打ち直せば送る", async ({
    page,
    server,
  }) => {
    // 待ち受けは Cookie が無い要求に「404 page not found」の平文を返す。本文が JSON で
    // ないからといって例外で処理が途切れると、失敗が画面に出ない。
    //
    // 404 は待っても直らない（Cookie か Origin が合わない。多くは dwloc を起動し直して、
    // この画面の Cookie が古くなったとき）。以前は送り直し続け、画面は「少し置いてから自動で
    // もう一度送ります」と言い続けた。送り直しは止め、訳は抱えたまま（閉じる前の引き止めも
    // 効く）、まだ入っていない訳を写せるように並べる。打ち直せば、その1回は送る。
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    const context = page.context();
    const cookies = await context.cookies();
    expect(cookies.length).toBeGreaterThan(0);
    await context.clearCookies();
    const posts = [];
    page.on("request", (req) => {
      if (new URL(req.url()).pathname === "/api/rows") {
        posts.push(req);
      }
    });

    const typed = "さようなら";
    const n = SAMPLE_LINES.goodbye;
    await typeTranslation(page, n, typed);
    const refused = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/rows");
    await closeEditor(page);
    expect((await refused).status()).toBe(404);

    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_failed"));
    await expect(saveState(page)).toHaveClass(FAILED);
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.save_refused", { status: 404 }));
    await expect(rowByLine(page, n)).toHaveClass(UNSAVED);
    await expectUnsent(page, n, keyFor(SAMPLE.goodbye.source), typed);
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);

    // 時計を進めても、送り直さない。
    await page.clock.runFor(120_000);
    await expectNoNewPost(page, posts, 1);
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.save_refused", { status: 404 }));

    // Cookie が戻ったあと、打ち直せばその訳を送る。
    await context.addCookies(cookies);
    await openEditor(page, n);
    await editor(page).press("End");
    await editor(page).pressSequentially("。");
    const accepted = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/rows");
    await closeEditor(page);
    expect((await accepted).status()).toBe(200);
    await waitForSaved(page);
    await expect(page.locator("#message")).toHaveText("");
    await expect(unsent(page)).toBeHidden();
    expectOnlyChanged(before, await server.readRoot(workingRel), {
      [n]: withTranslation(splitLines(before)[n - 1], `${typed}。`),
    });
  });

  // 400（要求の形が違う）と 415（本文が JSON でない）も待っても直らない。画面と待ち受けの版が
  // 食い違ったときなどに起きる。送り直さず、状態コードを添えて案内する。
  for (const c of [
    {
      status: 400,
      contentType: "application/json",
      body: () => JSON.stringify({ message: msg("ja", "error.bad_request") }),
    },
    { status: 415, contentType: "text/plain; charset=utf-8", body: () => msg("ja", "error.not_json") },
  ]) {
    test(`${c.status} が返ったら、送り直さず、状態コードを添えて案内する`, async ({ page, server }) => {
      await openPaused(page, server);
      const before = await server.readRoot(workingRel);
      const posts = [];
      await page.route("**/api/rows", (route) => {
        posts.push(route.request().postDataJSON());
        return route.fulfill({ status: c.status, contentType: c.contentType, body: c.body() });
      });

      const typed = "さようなら。";
      const n = SAMPLE_LINES.goodbye;
      await typeTranslation(page, n, typed);
      await closeEditor(page);
      await expect(page.locator("#message")).toHaveText(msg("ja", "ui.save_refused", { status: c.status }));
      await expect(saveState(page)).toHaveText(msg("ja", "ui.save_failed"));
      await expectUnsent(page, n, keyFor(SAMPLE.goodbye.source), typed);

      await page.clock.runFor(120_000);
      await expectNoNewPost(page, posts, 1);
      expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
    });
  }

  // 200 の応答が壊れていて、画面が受け止めるところで投げても、黙らない。未保存の訳は
  // 抱えたまま、失敗を出して送り直しへ回す（app.js の flush の最後の catch）。
  test("保存の応答を受け止めるところで投げても、訳を抱えたまま失敗を出し、送り直す", async ({ page, server }) => {
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    let broken = true;
    await page.route("**/api/rows", (route) =>
      broken
        ? route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ results: 5 }) })
        : route.continue(),
    );

    const typed = "さようなら。";
    const n = SAMPLE_LINES.goodbye;
    await typeTranslation(page, n, typed);
    await closeEditor(page);
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.save_failed_detail"));
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
    await expect(rowByLine(page, n)).toHaveClass(UNSAVED);

    broken = false;
    await page.clock.runFor(retryDelays[0]);
    await waitForSaved(page);
    expectOnlyChanged(before, await server.readRoot(workingRel), {
      [n]: withTranslation(splitLines(before)[n - 1], typed),
    });
  });

  test("作業コピーを書けないあいだ（503）は抱えて送り直し、書けるようになるとファイルに入る", async ({
    page,
    server,
  }) => {
    // Windows ではゲームがホットリロードで作業コピーを開いている最中の書き込みが落ちる。
    // 待てば直る失敗なので、画面は訳を抱えて送り直す（app.js の retryDelays の注記）。
    // ここでは本物の待ち受けに本物の書き込み失敗を起こさせる。
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    const restore = await makeUnwritable(server, workingRel);
    try {
      const typed = "さようなら。";
      const n = SAMPLE_LINES.goodbye;
      await typeTranslation(page, n, typed);
      const refused = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/rows");
      await closeEditor(page);
      expect((await refused).status()).toBe(503);

      await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
      await expect(page.locator("#message")).toHaveText(msg("ja", "error.save_failed"));
      await expect(rowByLine(page, n)).toHaveClass(UNSAVED);
      await expect(translationCell(page, n)).toHaveText(typed);
      expect((await server.readRoot(workingRel)).equals(before)).toBe(true);

      await restore();
      const accepted = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/rows");
      await page.clock.runFor(retryDelays[0]);
      expect((await accepted).status()).toBe(200);
      await waitForSaved(page);
      await expect(page.locator("#message")).toHaveText("");
      expectOnlyChanged(before, await server.readRoot(workingRel), {
        [n]: withTranslation(splitLines(before)[n - 1], typed),
      });
    } finally {
      await restore();
    }
  });

  test("送れなかった訳を捨てて切り替えたら、新しいロケールの画面に失敗を出さず、送り直しもしない", async ({
    page,
    server,
  }) => {
    // 送りかけの保存の失敗を新しいロケールの画面に出すと、誰も触っていない he の
    // 画面で「保存できませんでした」と出て、しかも送るものが無いのに送り直しを始める
    // （app.js の flush の世代の注記）。
    //
    // 切り替えは、送っている保存が返るのを待ってから決める（app.js の askDiscard）。
    // 以前は返る前に尋ねて移ったので、失敗は he の画面で返った。いまは失敗が ja の画面で
    // 返り、それでも残った訳を「消えます」と尋ね、受けてから移る。移ったあとの he の
    // 画面に失敗を出さず、捨てると答えた訳を送り直さないことを見る。
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    const posts = [];
    let release;
    const held = new Promise((resolve) => {
      release = resolve;
    });
    await page.route("**/api/rows", async (route) => {
      posts.push(route.request().postDataJSON());
      await held;
      await route.abort("connectionrefused");
    });
    const dialogs = [];
    page.on("dialog", (dialog) => {
      dialogs.push(dialog.message());
      return dialog.accept();
    });

    await typeTranslation(page, SAMPLE_LINES.goodbye, "さようなら。");
    await closeEditor(page);
    await expect.poll(() => posts.length).toBe(1);

    // 送っている最中に he を選ぶ。返るまでは尋ねず、移りもしない。
    await page.locator("#locale").selectOption("he");
    await page.clock.runFor(localeDelay);
    expect(dialogs).toHaveLength(0);
    await expect(page.locator("#file-path")).toHaveText(`${msg("ja", "ui.file")}: ${workingRel}`);
    release();

    // 届かなかったので訳は残る。尋ねて（受けて）から he へ移る。
    await expect(page.locator("#file-path")).toHaveText(`${msg("ja", "ui.file")}: Translations/he/strings.csv`);
    expect(dialogs).toEqual([msg("ja", "ui.switch_confirm")]);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_clean"));
    await expect(page.locator("#message")).toHaveText("");
    // 尋ねる前に、もう1度だけ送った（それも届かなかった）。移ったあとは送り直しも始めない。
    expect(posts).toHaveLength(2);
    await page.clock.runFor(60_000);
    await expectNoNewPost(page, posts, 2);
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);
  });

  test("送るものが無くなったら、「もう一度試しています」を出し続けない", async ({ page, server }) => {
    // 保存できないあいだに、打った訳を消して元の値へ戻した。送るものはもう無く、
    // ファイルは画面と同じなので、閉じても何も失われない。それでも帯が「保存できません
    // （もう一度試しています）」のままだと、翻訳者は存在しない失敗を直しにいく。
    // app.js の scheduleRetry は「送るものが残っていない」ときに saveError を倒すと
    // 書いているが、以前は送り直しの時計が flush の早い戻りに当たると、そこを通らなかった。
    await openPaused(page, server);
    const posts = [];
    await page.route("**/api/rows", (route) => {
      posts.push(route.request().postDataJSON());
      return route.abort("connectionrefused");
    });

    const n = SAMPLE_LINES.goodbye;
    await typeTranslation(page, n, "さようなら。");
    await closeEditor(page);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));

    // ファイルにある値（空）へ打ち戻す。未保存の控えから外れる。
    await typeTranslation(page, n, SAMPLE.goodbye.ja);
    await closeEditor(page);
    await expect(rowByLine(page, n)).not.toHaveClass(UNSAVED);

    // 送り直しの時計を切らせる。送るものが無いので要求は出ない。
    await page.clock.runFor(30_000);
    await expectNoNewPost(page, posts, 1);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_clean"));
    // 「保存できませんでした。…」の文も一緒に下ろす。帯と食い違う文を残さない。
    await expect(page.locator("#message")).toBeEmpty();
  });

  test("送っている間に打ち直した訳は、先の保存が返ったあとも欄に出し続ける", async ({ page, server }) => {
    // 先の保存が返ると、画面はその行の欄をファイルの値で描き直す（applyResults）。
    // そのとき未保存の控えにもっと新しい訳があれば、欄に出すべきはそちらである
    // （app.js の shownValue の注記「未保存があればそれ」）。古い値を出すと、次の保存が
    // 落ちたときに、翻訳者が見ている欄と送り直している訳が別物になる。
    await openPaused(page, server);
    const posts = [];
    let release;
    const held = new Promise((resolve) => {
      release = resolve;
    });
    await page.route("**/api/rows", async (route) => {
      posts.push(route.request().postDataJSON());
      if (posts.length === 1) {
        await held;
        return route.continue();
      }
      return route.abort("connectionrefused");
    });

    const n = SAMPLE_LINES.goodbye;
    await typeTranslation(page, n, "さようなら");
    await closeEditor(page);
    await expect.poll(() => posts.length).toBe(1);
    // 1つ目がまだ返っていないうちに打ち直す。
    await typeTranslation(page, n, "さようなら。");
    await closeEditor(page);
    release();
    await expect.poll(async () => (await server.readRootText(workingRel)).includes(",さようなら\r\n")).toBe(true);
    // 先の保存の応答を受け止め終えたこと（打ち直したぶんが未保存として残っている）を待つ。
    // 受け止める前に時計を進めると、打ち直したぶんを送る時計がまだ無い。
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));

    // 打ち直したぶんを自動保存で送らせる。これは落ちる。
    await page.clock.runFor(autosaveDelay);
    await expect.poll(() => posts.length).toBe(2);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
    expect(posts[1].edits).toEqual([expect.objectContaining({ line: n, translation: "さようなら。" })]);
    await expect(rowByLine(page, n)).toHaveClass(UNSAVED);
    await expect(translationCell(page, n)).toHaveText("さようなら。");
  });
});

test.describe("行ごとに断られたとき", () => {
  test("断られた行は理由を添えて残し、自動保存の対象から外す", async ({ app, server }) => {
    // 断られた行を未保存のまま抱えると、同じ要求を投げ続けることになる。捨てると
    // 訳が消える。だから値ごと「保存できない行」に移し、送る対象からだけ外す。
    const before = await server.readRoot(workingRel);
    const posts = [];
    await app.route("**/api/rows", (route) => {
      posts.push(route.request().postDataJSON());
      return misplace(route, SAMPLE_LINES.goodbye);
    });

    const typed = "さようなら。";
    const n = SAMPLE_LINES.goodbye;
    await typeTranslation(app, n, typed);
    await closeEditor(app);

    await expect(rowByLine(app, n)).toHaveClass(SAVE_FAILED);
    await expect(rowByLine(app, n)).not.toHaveClass(UNSAVED);
    await expect(rowByLine(app, n).locator(".row-note")).toHaveText(rowMovedNote());
    await expect(translationCell(app, n)).toHaveText(typed);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));
    await expect(saveState(app)).toHaveClass(FAILED);
    // 1行も書けなかったことは、待ち受けの文面のまま出す。
    await expect(app.locator("#message")).toHaveText(msg("ja", "error.no_row_saved"));
    expect((await server.readRoot(workingRel)).equals(before)).toBe(true);

    // 別の行を書き換える。送るのはその行だけで、断られた行は混ぜない。
    const other = SAMPLE_LINES.wonderful;
    await typeTranslation(app, other, "すごい！");
    await closeEditor(app);
    await expect.poll(() => posts.length).toBe(2);
    expect(posts[1].edits.map((edit) => edit.line)).toEqual([other]);
    await expect(rowByLine(app, other)).not.toHaveClass(UNSAVED);
    expectOnlyChanged(before, await server.readRoot(workingRel), {
      [other]: withTranslation(splitLines(before)[other - 1], "すごい！"),
    });
    // 断られた行はそのまま残る。
    await expect(rowByLine(app, n)).toHaveClass(SAVE_FAILED);
    await expect(translationCell(app, n)).toHaveText(typed);
  });

  test("一部の行だけ断られたときは、保存できた行だけがファイルに入り、断られた行が残る", async ({
    page,
    server,
  }) => {
    // 1行の失敗で残り全部を巻き添えにしない（internal/web の rowResult の注記）。
    // 200 の応答の中の saved: false は、その行だけを「保存できない行」にする。
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    const posts = [];
    await page.route("**/api/rows", (route) => {
      posts.push(route.request().postDataJSON());
      if (posts.length === 1) {
        return route.abort("connectionrefused");
      }
      return misplace(route, SAMPLE_LINES.goodbye);
    });

    // 1行目の保存を落として控えに残し、2行目を足して一緒に送らせる。
    const hello = SAMPLE_LINES.hello;
    const goodbye = SAMPLE_LINES.goodbye;
    await typeTranslation(page, hello, "もしもーし？");
    await closeEditor(page);
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_retrying"));
    await typeTranslation(page, goodbye, "さようなら。");
    await closeEditor(page);
    await expect.poll(() => posts.length).toBe(2);
    expect(posts[1].edits.map((edit) => edit.line)).toEqual([hello, goodbye]);

    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_failed"));
    await expect(rowByLine(page, hello)).not.toHaveClass(UNSAVED);
    await expect(rowByLine(page, hello)).not.toHaveClass(SAVE_FAILED);
    await expect(translationCell(page, hello)).toHaveText("もしもーし？");
    await expect(rowByLine(page, goodbye)).toHaveClass(SAVE_FAILED);
    await expect(rowByLine(page, goodbye).locator(".row-note")).toHaveText(rowMovedNote());
    await expect(translationCell(page, goodbye)).toHaveText("さようなら。");
    // 保存できた応答なので、要求の失敗の文は消えている。
    await expect(page.locator("#message")).toHaveText("");
    expectOnlyChanged(before, await server.readRoot(workingRel), {
      [hello]: withTranslation(splitLines(before)[hello - 1], "もしもーし？"),
    });
  });

  test("保存できなかった訳は、入力欄を開き直しても出し、書き換えると未保存に戻って送られる", async ({
    page,
    server,
  }) => {
    // 開き直した入力欄に古い保存値が入ると、1字打った瞬間に保存できなかった訳が
    // 黙って上書きされる（app.js の shownValue の注記）。
    // 時計を止めるのは、打ったあとの確かめのあいだに自動保存が走らないようにするため。
    await openPaused(page, server);
    const before = await server.readRoot(workingRel);
    await page.route("**/api/rows", (route) => misplace(route, SAMPLE_LINES.goodbye));
    const n = SAMPLE_LINES.goodbye;
    await typeTranslation(page, n, "さようなら");
    await closeEditor(page);
    await expect(rowByLine(page, n)).toHaveClass(SAVE_FAILED);

    await openEditor(page, n);
    await expect(editor(page)).toHaveValue("さようなら");

    // 1字足す。「保存できない行」から外れ、未保存として数えられる。
    await editor(page).press("End");
    await editor(page).pressSequentially("。");
    await expect(editor(page)).toHaveValue("さようなら。");
    await expect(rowByLine(page, n)).toHaveClass(UNSAVED);
    await expect(rowByLine(page, n)).not.toHaveClass(SAVE_FAILED);
    await expect(rowByLine(page, n).locator(".row-note")).toBeHidden();
    await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));

    // 断る原因が消えていれば、次の保存で入る。
    await page.unroute("**/api/rows");
    await closeEditor(page);
    await waitForSaved(page);
    expectOnlyChanged(before, await server.readRoot(workingRel), {
      [n]: withTranslation(splitLines(before)[n - 1], "さようなら。"),
    });
  });

  // 行ごとに断られた訳も、画面の中にしか無い。待ち受けに届かなくなったら、送り直している
  // 訳と一緒に、まだファイルに入っていない訳として並べる。並びは行の順。
  test("保存できない行があるときに待ち受けに届かなくなったら、その訳も行の順に一覧へ並べる", async ({
    page,
    server,
  }) => {
    await openPaused(page, server);
    let calls = 0;
    await page.route("**/api/rows", (route) => {
      calls += 1;
      return calls === 1 ? misplace(route, SAMPLE_LINES.goodbye) : route.abort("connectionrefused");
    });
    const goodbye = SAMPLE_LINES.goodbye;
    const hello = SAMPLE_LINES.hello;
    await typeTranslation(page, goodbye, "さようなら。");
    await closeEditor(page);
    await expect(rowByLine(page, goodbye)).toHaveClass(SAVE_FAILED);
    // 行ごとに断られただけなら、待ち受けには届いている。一覧は出さない。
    await expect(unsent(page)).toBeHidden();

    await typeTranslation(page, hello, "もしもーし？");
    await closeEditor(page);
    await expect(page.locator("#message")).toHaveText(msg("ja", "ui.unreachable"));
    await expect(page.locator("#unsent-list > li")).toHaveText([
      `${msg("ja", "ui.unsent_line", { line: hello })} ${keyFor(SAMPLE.hello.source)}: もしもーし？`,
      `${msg("ja", "ui.unsent_line", { line: goodbye })} ${keyFor(SAMPLE.goodbye.source)}: さようなら。`,
    ]);
  });

  test("保存できなかった訳は、読み直しを取り消せば残る", async ({ app }) => {
    // 読み直すと保存できなかった訳は消える。だから消す前に必ず尋ね、取り消せば
    // 何も変わらない（app.js の askDiscard）。
    await app.route("**/api/rows", (route) => misplace(route, SAMPLE_LINES.goodbye));
    const n = SAMPLE_LINES.goodbye;
    await typeTranslation(app, n, "さようなら。");
    await closeEditor(app);
    await expect(rowByLine(app, n)).toHaveClass(SAVE_FAILED);

    const dialogs = [];
    app.on("dialog", (dialog) => {
      dialogs.push({ type: dialog.type(), message: dialog.message() });
      return dialog.dismiss();
    });
    await app.locator("#reload").click();
    await expect.poll(() => dialogs).toEqual([{ type: "confirm", message: msg("ja", "ui.discard_confirm") }]);

    await expect(translationCell(app, n)).toHaveText("さようなら。");
    await expect(rowByLine(app, n)).toHaveClass(SAVE_FAILED);
    await expect(rowByLine(app, n).locator(".row-note")).toHaveText(rowMovedNote());
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));
  });

  test("保存できなかった行は、絞り込みにも検索にも当たらなくても隠さない", async ({ app }) => {
    // 隠すと、直すべき行が画面から消え、翻訳者は消えたことに気づけない
    // （app.js 冒頭の「絞り込みと検索について守ること」）。
    const n = SAMPLE_LINES.wonderful;
    await app.route("**/api/rows", (route) => misplace(route, n));
    await typeTranslation(app, n, "すごい！");
    await closeEditor(app);
    await expect(rowByLine(app, n)).toHaveClass(SAVE_FAILED);

    // wonderful には「未翻訳」が付いていない。絞り込みが効いていることは hello で見る。
    await app.locator("#filters .chip", { hasText: msg("ja", "category.untranslated") }).locator("input").check();
    await expect(rowByLine(app, SAMPLE_LINES.hello)).toBeHidden();
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toBeVisible();
    await expect(rowByLine(app, n)).toBeVisible();

    // 検索語にも当たらない。
    await app.locator("#search").fill("zzzz");
    await expect(rowByLine(app, SAMPLE_LINES.goodbye)).toBeHidden();
    await expect(rowByLine(app, n)).toBeVisible();
    await expect(app.locator("#shown")).toHaveText(msg("ja", "ui.shown", { count: 1 }));
  });

  test("保存できなかった行だけが残っていても、頁を閉じようとすると引き止める", async ({ app }) => {
    // 断られた行は未保存の控えから外れている。引き止めが未保存の控えしか見ないと、
    // ここが訳の残っている最後の場所なのに、黙って閉じられる。
    await app.route("**/api/rows", (route) => misplace(route, SAMPLE_LINES.goodbye));
    await typeTranslation(app, SAMPLE_LINES.goodbye, "さようなら。");
    await closeEditor(app);
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));

    const dialogs = [];
    app.on("dialog", (dialog) => {
      dialogs.push(dialog.type());
      return dialog.dismiss();
    });
    await app.close({ runBeforeUnload: true });
    await expect.poll(() => dialogs).toEqual(["beforeunload"]);
    expect(app.isClosed()).toBe(false);
    await expect(translationCell(app, SAMPLE_LINES.goodbye)).toHaveText("さようなら。");
  });

  test("競合で描き直しても、保存できなかった訳と理由は同じ行に残る", async ({ app, server }) => {
    // 競合（409）のあとは読み直した内容で一覧を描き直す。描き直しで古い保存値が
    // 出ると、保存できなかった訳が画面から消え、次の1字で上書きされる。
    const n = SAMPLE_LINES.goodbye;
    await app.route("**/api/rows", (route) => misplace(route, n));
    await typeTranslation(app, n, "さようなら。");
    await closeEditor(app);
    await expect(rowByLine(app, n)).toHaveClass(SAVE_FAILED);
    await app.unroute("**/api/rows");

    // よそが hello の訳だけを書き換えた（行はずれていない）。
    const external = Buffer.from(
      (await server.readRoot(workingRel)).toString("utf8").replace(`,${SAMPLE.hello.ja}\n`, ",やあ？\n"),
      "utf8",
    );
    await server.writeRoot(workingRel, external);

    // 別の行を保存すると 409 になり、画面は読み直した内容で描き直してから保存し直す。
    const other = SAMPLE_LINES.wonderful;
    await typeTranslation(app, other, "すごい！");
    await closeEditor(app);
    await expect(translationCell(app, SAMPLE_LINES.hello)).toHaveText("やあ？");
    await expect
      .poll(async () => (await server.readRoot(workingRel)).equals(external), { timeout: 10_000 })
      .toBe(false);

    await expect(translationCell(app, n)).toHaveText("さようなら。");
    await expect(rowByLine(app, n)).toHaveClass(SAVE_FAILED);
    await expect(rowByLine(app, n).locator(".row-note")).toHaveText(rowMovedNote());
    // 描き直しのあとも、保存できなかった行は送らない。入ったのは wonderful だけ。
    expectOnlyChanged(external, await server.readRoot(workingRel), {
      [other]: withTranslation(splitLines(external)[other - 1], "すごい！"),
    });
  });

  test("描き直しで行がずれても、保存できなかった訳を別のキーの行に出さない", async ({ app, server }) => {
    // 409 のあとの載せ直しは行番号ではなくキーで行う（app.js の remap の注記。行番号の
    // ままだと訳が別の行に入り、その行にもとからあった訳が消える。実際に起きた）。
    // 以前は未保存の控えだけをそうしていて、保存できなかった訳（state.failed）は行番号の
    // まま残った。すると、よそが上に1行足しただけで、goodbye の訳が hello の行に
    // 「保存できない行」として出た。そのまま hello の行を1字直すと、goodbye の訳が
    // hello のキーへ保存され、hello の訳は消えた。
    const n = SAMPLE_LINES.goodbye;
    const failedText = "さようなら。";
    await app.route("**/api/rows", (route) => misplace(route, n));
    await typeTranslation(app, n, failedText);
    await closeEditor(app);
    await expect(rowByLine(app, n)).toHaveClass(SAVE_FAILED);
    await app.unroute("**/api/rows");

    // よそが節点の見出しのすぐ下に1行足した。hello 以降が1行ずつ下がる。
    const s = SAMPLE;
    await server.writeRoot(
      workingRel,
      workingCopy([
        "",
        "# ===== Level 1: Ryan (Sunny) =====",
        "# --- intro: Ryan_1_intro ---",
        { source: "Added line.", translation: "足した行" },
        { ...s.hello, translation: s.hello.ja },
        { ...s.goodbye, translation: s.goodbye.ja },
        { ...s.wonderful, translation: s.wonderful.ja },
        "",
      ]),
    );

    // wonderful（7行目）を保存すると 409。読み直した内容では 8行目へ移る。
    await typeTranslation(app, SAMPLE_LINES.wonderful, "すごい！");
    await closeEditor(app);
    await expect
      .poll(async () => (await server.readRootText(workingRel)).includes(",すごい！\n"), { timeout: 10_000 })
      .toBe(true);
    // 保存の応答を受け止め終えるまで待つ。保存できない行が残るので「保存済み」にはならない。
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_failed"));

    // hello はいま6行目。そこには hello の訳だけが出る。
    const helloNow = rowByLine(app, SAMPLE_LINES.hello + 1);
    await expect(helloNow.locator(".cell.source")).toHaveText(s.hello.source);
    await expect(helloNow.locator(".cell.translation")).toHaveText(s.hello.ja);
    await expect(helloNow).not.toHaveClass(SAVE_FAILED);
    // 保存できなかった訳は捨てずに、goodbye の行（いま7行目）か行き先の無い訳の欄に出る。
    await expect
      .poll(async () => {
        const goodbyeNow = await rowByLine(app, SAMPLE_LINES.goodbye + 1).locator(".cell.translation").textContent();
        const orphans = await app.locator("#orphans-list").textContent();
        return goodbyeNow === failedText || orphans.includes(failedText);
      })
      .toBe(true);
  });
});

// 待ち受けは、操作が無いまま --idle-timeout（既定 30m）たつと自分で終わる（README）。
// そのあともタブは開いたままで、訳の欄も打てる。以前は、打った訳の保存が届かなくても
// 「少し置いてから自動でもう一度送ります」と言い続けた。起動し直すと URL（ポートと
// トークン）が変わるので、このタブの訳は待っても送られず、手で写すしかない。届かないことと
// 起動し直し方を言い、まだファイルに入っていない訳を写せるように一覧に並べる。
// 時間切れで終わる方針そのものは変えない（誰も開けない待ち受けを残さない）。
test.describe("待ち受けが操作の無いまま時間切れで終わったあと", () => {
  test.use({ dwloc: { idleTimeout: "2s" } });

  test("届かないことを案内し、まだファイルに入っていない訳を一覧に並べて写せるようにする", async ({ app, server }) => {
    // 待ち受けが自分で終わるのを待つ。時間切れは失敗ではないので、終了コードは 0。
    expect((await server.exited).code).toBe(0);

    const typed = "さようなら。";
    const n = SAMPLE_LINES.goodbye;
    const key = keyFor(SAMPLE.goodbye.source);
    await typeTranslation(app, n, typed);
    await closeEditor(app);
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.unreachable"));
    await expect(saveState(app)).toHaveText(msg("ja", "ui.save_retrying"));
    await expect(rowByLine(app, n)).toHaveClass(UNSAVED);
    await expectUnsent(app, n, key, typed);

    // 写せる。一覧の訳は字として出ているので、選べばそのまま写せる。
    await app.locator("#unsent-list > li .note-value").selectText();
    expect(await app.evaluate(() => window.getSelection().toString())).toBe(typed);

    // 読み直しも届かない。受けても、読み直しを勧める「読み込めませんでした。読み直して
    // ください。」ではなく、届かないことを言う。抱えている訳も一覧もそのまま残る。
    const dialogs = [];
    app.on("dialog", (dialog) => {
      dialogs.push(dialog.message());
      return dialog.accept();
    });
    await app.locator("#reload").click();
    await expect.poll(() => dialogs).toEqual([msg("ja", "ui.discard_confirm")]);
    await expect(app.locator("#list")).toHaveAttribute("aria-busy", "false");
    await expect(app.locator("#message")).toHaveText(msg("ja", "ui.unreachable"));
    await expect(translationCell(app, n)).toHaveText(typed);
    await expectUnsent(app, n, key, typed);
  });
});
