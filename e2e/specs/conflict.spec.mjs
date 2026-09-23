// 競合（409）と、行き先の無い訳を見る。
//
// 保存の要求は、画面が読んだときのファイルの版（ファイル全体の SHA-256）を載せる。
// 手前でファイルが変わっていれば、待ち受けは1バイトも書かずに 409 といまの中身を返す
// （internal/web の rows.go と doc.go の「訳を失わない」）。作業コピーはゲーム内の Mod も
// 書くファイルなので、翻訳者が打っているあいだに書き換わることは実際に起きる。
//
// そのとき画面が守るのは、app.js の冒頭にある「409 で未保存の編集を捨てない。読み直した
// 内容を出したうえで、どちらを載せるかを人に選ばせる。黙って上書きも、黙って破棄も
// しない」である。ここでは、それを画面の表示だけでなくファイルのバイトで確かめる。
// 触っていない行が1バイトも動かないことも、期待するファイルを丸ごと組んで比べて見る。
//
// 自動保存の時計は page.clock で止めておく。止めないと、打ってから「よその書き換え」を
// 挟むまでのあいだに 1.5 秒の時計が切れる遅い機械では、409 にならずに普通に保存され、
// 見たい場面そのものが作れない。保存は Escape（欄から離れると待たずに送る）か、
// 時計を進めて走らせる。
import { expect, test } from "../support/test.mjs";
import { msg } from "../support/catalog.mjs";
import { SAMPLE, SAMPLE_LINES, keyFor, sampleRepo, workingCopy } from "../support/repo.mjs";
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
const s = SAMPLE;
const L = SAMPLE_LINES;

// 時計の始まりと、止める時刻。止める時刻は画面を開き終えたあとで十分先ならよい。
const clockStart = new Date("2026-01-01T00:00:00Z");
const clockPause = new Date("2026-01-01T01:00:00Z");

// pastAutosave は自動保存の時計（internal/web の autosaveDelay、1.5 秒）より長い時間。
const pastAutosave = 2_000;

// pastRetries は送り直しの間隔（app.js の retryDelays、最後は 30 秒）を使い切る時間。
// 「送らない」を確かめるときは、ここまで進めても送らないことを見る。
const pastRetries = 60_000;

// ---- 見本の組み立て ----
//
// 既定の見本（support/repo.mjs の sampleWorkingCopy）と同じ形を、行を差し替えられる
// ように組み直す。よそが書き換えたファイルも、期待する保存後のファイルも、ここで組む。

const head = ["", "# ===== Level 1: Ryan (Sunny) =====", "# --- intro: Ryan_1_intro ---"];

// copy は ja の作業コピーを組む。rows は見出しのあとに並ぶデータ行（物理行 5 から）。
function copy(...rows) {
  return workingCopy([...head, ...rows, ""]);
}

const hello = (translation = s.hello.ja) => ({ ...s.hello, translation });
const goodbye = (translation = s.goodbye.ja) => ({ ...s.goodbye, translation });
const wonderful = (translation = s.wonderful.ja) => ({ ...s.wonderful, translation });

// extra は、よそが足す行。再生順には無いが列の数はそろっているので、編集できる行になる。
const extra = { source: "Hold on.", speaker: "Ryan", order: "4", translation: "待って。" };

// original は起動したときの作業コピー（既定の見本と同じバイト）。
const original = copy(hello(), goodbye(), wonderful());

// ---- 画面の操作 ----

// openPaused は時計を止めた状態で画面を開く。
//
// ついでに /api/rows への POST を頁の中で数える仕掛けを入れる（rowPosts）。
// 「送らない」ことを確かめるのに、ファイルが変わらないことだけでは足りない。
// 送った要求が待ち受けに届く前に読めば、変わっていないように見えるからである。
// 頁の中で数えれば、時計を進め終えた時点で送ったかどうかが決まっている
// （flush は時計の中から同期で fetch を呼ぶ）。
//
// 最後に、見本が手元の組み立てと同じバイトであることを確かめる。期待するバイトは
// すべて copy から作るので、ここがずれていると比べる意味が無くなる。
async function openPaused(page, server, initial = original) {
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
  await page.clock.install({ time: clockStart });
  await openApp(page, server);
  await page.clock.pauseAt(clockPause);
  await expectFile(server, initial);
}

// rowPosts は、ここまでに画面が /api/rows へ送った POST の数。
function rowPosts(page) {
  return page.evaluate(() => window.__rowPosts);
}

// nextSave は次の保存の応答を待つ。操作の前に呼んでおく。
function nextSave(page) {
  return page.waitForResponse(
    (res) => new URL(res.url()).pathname === "/api/rows" && res.request().method() === "POST",
  );
}

// expectFile は作業コピーが text とバイト単位で同じであることを確かめる。
//
// 文字列でも比べるのは、食い違ったときにどの行かを読めるようにするためである。
async function expectFile(server, text) {
  const got = await server.readRoot(workingRel);
  expect(got.toString("utf8")).toBe(text);
  expect(got.equals(Buffer.from(text, "utf8")), "バイトが違う").toBe(true);
}

// raiseConflict は line に typed を打ち、送る前によそが external を書いた状態で送る。
//
// 待ち受けが 409 を返したことまで確かめる。409 を経ずに保存されると、ここで見たい
// 振る舞い（載せ直し・引き止め）を1つも通らないまま試験が通ってしまう。
async function raiseConflict(page, server, line, typed, external) {
  await typeTranslation(page, line, typed);
  await server.writeRoot(workingRel, external);
  const saving = nextSave(page);
  await editor(page).press("Escape");
  expect((await saving).status()).toBe(409);
}

const conflictBox = (page) => page.locator("#conflict");
const keepButton = (page) => page.locator("#conflict-keep");
const takeButton = (page) => page.locator("#conflict-take");
const orphansBox = (page) => page.locator("#orphans");

// conflictValues は競合している行の1言に並ぶ「ファイルの訳」と「あなたの訳」。
function conflictValues(page, line) {
  return rowByLine(page, line).locator(".row-note .note-value");
}

// ---- 競合したとき ----

// 409 は「1バイトも書いていない」という意味である。画面がここで黙って読み直すと、
// 翻訳者の訳は捨てられ、黙って送り直すと、よその訳が黙って上書きされる。どちらも
// 翻訳者が気づけないまま起きるので、画面は両方の訳を並べて人に選ばせなければならない。
test("同じ行をよそが書き換えていたら、1バイトも書かずに引き止めを出し、両方の訳を並べる", async ({ page, server }) => {
  await openPaused(page, server);
  const external = copy(hello(), goodbye("またね。"), wonderful());
  await raiseConflict(page, server, L.goodbye, "さようなら。", external);

  await expect(conflictBox(page)).toBeVisible();
  await expect(page.locator("#conflict-title")).toHaveText(msg("ja", "ui.conflict_title"));
  await expect(page.locator("#conflict-help")).toHaveText(msg("ja", "ui.conflict_help"));
  await expect(keepButton(page)).toHaveText(msg("ja", "ui.conflict_keep_mine"));
  await expect(takeButton(page)).toHaveText(msg("ja", "ui.conflict_take_file"));
  // 理由は待ち受けが返した文面をそのまま出す。
  await expect(page.locator("#message")).toHaveText(msg("ja", "error.conflict"));
  // 保存の状態は「保存済み」とも「未保存」とも言わない。
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_conflict"));
  await expect(saveState(page)).toHaveClass(/(^|\s)conflict(\s|$)/);

  // 欄には読み直したファイルの訳を出し、自分の訳は行の1言に並べる。どちらも画面から消さない。
  const row = rowByLine(page, L.goodbye);
  await expect(row).toHaveClass(/(^|\s)conflicted(\s|$)/);
  await expect(row.locator(".cell.translation")).toHaveText("またね。");
  await expect(conflictValues(page, L.goodbye)).toHaveText(["またね。", "さようなら。"]);
  await expect(row.locator(".row-note")).toContainText(msg("ja", "ui.conflict_file"));
  await expect(row.locator(".row-note")).toContainText(msg("ja", "ui.conflict_mine"));
  await expect(row.locator(".row-note .note-locked")).toHaveText(msg("ja", "ui.conflict_locked"));
  // 競合していない行は巻き込まない。
  await expect(rowByLine(page, L.hello)).not.toHaveClass(/(^|\s)conflicted(\s|$)/);
  await expect(rowByLine(page, L.wonderful)).not.toHaveClass(/(^|\s)conflicted(\s|$)/);

  // ファイルはよそが書いたままで、自分の訳は入っていない。
  await expectFile(server, external);
});

// 決まっていない行への入力は「自分の訳を載せる」にも「ファイルの訳を採る」にも取れ、
// どちらへ寄せても押したボタンと逆の結果になる道が残った（doc.go「競合している行は、
// 選ぶまで開かない」）。だから開かせない。開かない道が1つでも残っていれば、そこから
// 曖昧な状態に戻る。クリック・Tab・Enter の3つの入口を全部見る。
test("競合した行は、選ぶまでクリックでも Tab でも Enter でも開かない", async ({ page, server }) => {
  await openPaused(page, server);
  await raiseConflict(page, server, L.goodbye, "さようなら。", copy(hello(), goodbye("またね。"), wonderful()));
  await expect(conflictBox(page)).toBeVisible();

  // クリックしても入力欄は開かない。
  await translationCell(page, L.goodbye).click();
  await expect(editor(page)).toHaveCount(0);
  // Tab の行き先にもならない（打てない欄に焦点を止めない）。
  await expect(translationCell(page, L.goodbye)).not.toHaveAttribute("tabindex");

  // 上の行から Tab で進むと、競合した行を飛ばして次の行が開く。
  await openEditor(page, L.hello);
  await editor(page).press("Tab");
  await expect(rowByLine(page, L.wonderful).locator("textarea.editor")).toBeFocused();

  // Enter の行送りも、競合した行を飛ばす。飛ばさないと焦点が body へ落ちる。
  await openEditor(page, L.hello);
  await editor(page).press("Enter");
  await expect(rowByLine(page, L.wonderful).locator("textarea.editor")).toBeFocused();
  await expect(rowByLine(page, L.goodbye).locator("textarea.editor")).toHaveCount(0);

  // 開かなかった理由は行に出たまま。
  await expect(rowByLine(page, L.goodbye).locator(".note-locked")).toHaveText(msg("ja", "ui.conflict_locked"));
  await expect(conflictValues(page, L.goodbye)).toHaveText(["またね。", "さようなら。"]);
});

// 引き止めのあいだ自動保存は止まる（ui.conflict_help がそう言っている）。止まらないと、
// 人が選ぶ前によその版の上へ書き込むことになり、選ばせる意味が無くなる。止まっている
// あいだに別の行へ打った訳は、ファイルには無いが画面には残っていなければならない。
test("引き止めのあいだは自動保存が止まり、ほかの行に打った訳もファイルへ書かない", async ({ page, server }) => {
  await openPaused(page, server);
  const external = copy(hello(), goodbye("またね。"), wonderful());
  await raiseConflict(page, server, L.goodbye, "さようなら。", external);
  await expect(conflictBox(page)).toBeVisible();
  expect(await rowPosts(page)).toBe(1);

  await typeTranslation(page, L.wonderful, "すごい！");
  // 自動保存の時計も、送り直しの間隔も使い切るまで進め、欄からも離れる（離れると待たずに送る道）。
  await page.clock.runFor(pastRetries);
  await editor(page).press("Escape");

  expect(await rowPosts(page)).toBe(1);
  await expectFile(server, external);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_conflict"));
  await expect(translationCell(page, L.wonderful)).toHaveText("すごい！");
});

// 「自分の訳を上に載せる」は、読み直した版の上に自分の訳を載せる。よその変更を巻き戻す
// ことではない。よそが別の行も変えていたら、その行はよその変更のまま残らなければならない。
test("自分の訳を上に載せると、その訳がファイルへ入り、よそが変えたほかの行はそのまま残る", async ({ page, server }) => {
  await openPaused(page, server);
  const external = copy(hello("もしもし。"), goodbye("またね。"), wonderful());
  await raiseConflict(page, server, L.goodbye, "さようなら。", external);
  await expect(conflictBox(page)).toBeVisible();

  const saving = nextSave(page);
  await keepButton(page).click();
  expect((await saving).status()).toBe(200);
  await waitForSaved(page);

  await expectFile(server, copy(hello("もしもし。"), goodbye("さようなら。"), wonderful()));
  await expect(conflictBox(page)).toBeHidden();
  await expect(page.locator("#message")).toBeEmpty();
  await expect(rowByLine(page, L.goodbye)).not.toHaveClass(/(^|\s)conflicted(\s|$)/);
  await expect(translationCell(page, L.goodbye)).toHaveText("さようなら。");
  // 選び終えたら、また直せる。
  await openEditor(page, L.goodbye);
  await expect(editor(page)).toHaveValue("さようなら。");
});

// 「ファイルの訳を採る」で初めて自分の編集を捨てる。人が選んだ結果なので捨ててよいが、
// 捨てた訳が裏で送られてはならない（押したボタンと逆の結果になる）。ファイルはよそが
// 書いたまま1バイトも変わらないことを、送り直しの間隔を使い切るまで待って見る。
test("ファイルの訳を採ると、ファイルの訳が残り、自分の訳は送らない", async ({ page, server }) => {
  await openPaused(page, server);
  const external = copy(hello(), goodbye("またね。"), wonderful());
  await raiseConflict(page, server, L.goodbye, "さようなら。", external);
  await expect(conflictBox(page)).toBeVisible();

  await takeButton(page).click();
  await expect(conflictBox(page)).toBeHidden();
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_clean"));
  await expect(translationCell(page, L.goodbye)).toHaveText("またね。");

  await page.clock.runFor(pastRetries);
  expect(await rowPosts(page)).toBe(1);
  await expectFile(server, external);
  // 開き直すと、入力欄にはファイルの訳が入る。
  await openEditor(page, L.goodbye);
  await expect(editor(page)).toHaveValue("またね。");
});

// 引き止めのあいだも、競合していない行は打てる。そこで打った訳はブラウザーの中にしか
// 無い（自動保存は止まっている）。ui.conflict_help は「ほかの行に打った訳は、どちらを
// 選んでも残ります」と約束している。丸ごと置き換えていたころ、まさにここで訳が消えた。
for (const choice of [
  { name: "自分の訳を載せても", button: keepButton, goodbye: "さようなら。" },
  { name: "ファイルの訳を採っても", button: takeButton, goodbye: "またね。" },
]) {
  test(`選んでいるあいだに別の行へ打った訳は、${choice.name}ファイルへ入る`, async ({ page, server }) => {
    await openPaused(page, server);
    await raiseConflict(page, server, L.goodbye, "さようなら。", copy(hello(), goodbye("またね。"), wonderful()));
    await expect(conflictBox(page)).toBeVisible();

    await typeTranslation(page, L.wonderful, "すごい！");
    await editor(page).press("Escape");

    const saving = nextSave(page);
    await choice.button(page).click();
    expect((await saving).status()).toBe(200);
    await waitForSaved(page);

    await expectFile(server, copy(hello(), goodbye(choice.goodbye), wonderful("すごい！")));
    await expect(translationCell(page, L.wonderful)).toHaveText("すごい！");
  });
}

// 409 は「ファイル全体の版が合わない」という意味しか持たない。よそが触ったのが
// こちらの触っていない行だけなら、選ばせることは無い（doc.go「選ばせるのは、よそが
// 本当に書き換えた行だけ」）。そこで引き止めを出すと、翻訳者は毎回意味の無い選択を
// 迫られる。黙って上書きしているのではないことは、よその行が残ることで見る。
test("よそが別の行だけを書き換えたときは、引き止めを出さずに新しい版の上へ保存し直す", async ({ page, server }) => {
  await openPaused(page, server);
  const external = copy(hello(), goodbye(), wonderful("最高！"));
  await raiseConflict(page, server, L.goodbye, "さようなら。", external);

  // 読み直した版が描かれた（よその訳が出た）ことを待ってから見る。
  await expect(translationCell(page, L.wonderful)).toHaveText("最高！");
  await expect(conflictBox(page)).toBeHidden();
  await expect(page.locator("#message")).toBeEmpty();
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_pending", { count: 1 }));
  await expect(translationCell(page, L.goodbye)).toHaveText("さようなら。");

  // すぐには送らず、自動保存の時計を通す（よそが書き続けているあいだ 409 で回らないため）。
  const saving = nextSave(page);
  await page.clock.runFor(pastAutosave);
  expect((await saving).status()).toBe(200);
  await waitForSaved(page);
  await expectFile(server, copy(hello(), goodbye("さようなら。"), wonderful("最高！")));
});

// 自動保存は入力欄を開いたまま走る（入力が止まると保存する）。そこで 409 になり、選ばせる
// ことが無い（よそが触ったのは別の行）なら、翻訳者は打ち続けてよいはずである。以前は
// 読み直しの描き直しが入力欄を外し、焦点は body へ落ち、続けて打った字はどこにも入らないのに
// 保存の欄は「保存済み」と言った。これは app.js の keepAlways のコメントが「訳が落ちた」事故として
// 挙げている形そのもので、そちらは「触っている行は隠さない」で直してある（app.js の onConflict）。
test("よそが別の行だけを書き換えた 409 のあとも、打っている行の入力欄は閉じず、続けて打った字が訳に入る", async ({ page, server }) => {
  await openPaused(page, server);
  await typeTranslation(page, L.goodbye, "さような");
  await server.writeRoot(workingRel, copy(hello(), goodbye(), wonderful("最高！")));
  const saving = nextSave(page);
  // 入力が止まったので自動保存が走る。入力欄は開いたまま。
  await page.clock.runFor(pastAutosave);
  expect((await saving).status()).toBe(409);
  await expect(translationCell(page, L.wonderful)).toHaveText("最高！");
  // 描き直しで入力欄が外れても、その blur ですぐに送り直さない（doc.go「すぐに送らず
  // 自動保存の時計を通す」）。送ったのは 409 になった1回だけ。
  expect(await rowPosts(page)).toBe(1);

  // 打っていた行の入力欄に、焦点が残っている。
  await expect(rowByLine(page, L.goodbye).locator("textarea.editor")).toBeFocused();
  // 続けて打った字は、その行の訳に続く。
  await page.keyboard.type("ら。");
  await expect(editor(page)).toHaveValue("さようなら。");
  const expected = copy(hello(), goodbye("さようなら。"), wonderful("最高！"));
  await expect
    .poll(async () => {
      await page.clock.runFor(pastAutosave);
      return server.readRootText(workingRel);
    })
    .toBe(expected);
});

// 打っていた行そのものをよそが書き換えていたら、その行は競合になり、選ぶまで開かない
// （openEditor）。入力欄は開き直せないので、以前は焦点が body へ落ち、続けて打った字は
// 黙ってどこにも入らなかった（上の試験と同じ事故の形）。焦点を引き止めの枠へ移し、
// 次にすることを画面にも読み上げにも出す（app.js の onConflict）。
//
// ボタンへは移さない。打ち続けた Space や Enter がボタンを押し、よその訳を読まずに
// どちらかを選んでしまう。続けて打っても何も選ばれないことまで見る。
test("打っていた行そのものが競合したら、焦点を引き止めの枠へ移し、続けて打っても何も選ばない", async ({ page, server }) => {
  await openPaused(page, server);
  await typeTranslation(page, L.goodbye, "さような");
  const external = copy(hello(), goodbye("またね。"), wonderful());
  await server.writeRoot(workingRel, external);
  const saving = nextSave(page);
  // 入力が止まったので自動保存が走る。入力欄は開いたまま。
  await page.clock.runFor(pastAutosave);
  expect((await saving).status()).toBe(409);

  await expect(conflictBox(page)).toBeVisible();
  await expect(conflictValues(page, L.goodbye)).toHaveText(["またね。", "さような"]);
  // 競合した行は開き直さない。焦点は body ではなく、引き止めの枠にある。
  await expect(editor(page)).toHaveCount(0);
  await expect(conflictBox(page)).toBeFocused();
  expect(await page.evaluate(() => document.activeElement === document.body)).toBe(false);

  // 打ち続けても（Space と Enter を含む）どちらのボタンも押されない。
  await page.keyboard.type("ら ");
  await page.keyboard.press("Enter");
  await page.keyboard.press("Space");
  await page.clock.runFor(pastAutosave);
  await expect(conflictBox(page)).toBeVisible();
  await expect(conflictValues(page, L.goodbye)).toHaveText(["またね。", "さような"]);

  // Tab で最初のボタンへ進める。
  await conflictBox(page).focus();
  await page.keyboard.press("Tab");
  await expect(keepButton(page)).toBeFocused();

  // 何も選んでいないので、送ったのは 409 になった1回だけ。ファイルはよそが書いたまま。
  expect(await rowPosts(page)).toBe(1);
  await expectFile(server, external);
});

// 読み直すまでのあいだに、よそが行を足すと同じ行番号が別の台詞を指す。行番号で
// 載せ直すと、訳が別の行へ入り、その行にもとからあった訳が消える（app.js の remap に
// 「実際に起きた」とある）。載せ直しはキーで行う。
test("よそが行を足して行番号がずれても、訳はキーで元の台詞の行へ入る", async ({ page, server }) => {
  await openPaused(page, server);
  const external = copy(hello(), extra, goodbye(), wonderful());
  await raiseConflict(page, server, L.goodbye, "さようなら。", external);

  // 6行目はよそが足した行になり、goodbye は7行目へずれた。
  await expect(translationCell(page, 6)).toHaveText(extra.translation);
  await expect(translationCell(page, 7)).toHaveText("さようなら。");
  await expect(conflictBox(page)).toBeHidden();

  const saving = nextSave(page);
  await page.clock.runFor(pastAutosave);
  expect((await saving).status()).toBe(200);
  await waitForSaved(page);
  // 足された行は1バイトも変わらず、訳は goodbye の行へ入る。
  await expectFile(server, copy(hello(), extra, goodbye("さようなら。"), wonderful()));
});

// ずれた先の台詞もよそが書き換えていたら、選ばせる場所はずれた先の行である。
// 元の行番号で選ばせると、別の台詞の行に「あなたの訳」が並び、どちらを押しても
// 別の行を書き換えることになる。
test("行がずれた先の台詞もよそが書き換えていたら、ずれた先の行で選ばせる", async ({ page, server }) => {
  await openPaused(page, server);
  const external = copy(hello(), extra, goodbye("またね。"), wonderful());
  await raiseConflict(page, server, L.goodbye, "さようなら。", external);

  await expect(conflictBox(page)).toBeVisible();
  await expect(rowByLine(page, 7)).toHaveClass(/(^|\s)conflicted(\s|$)/);
  await expect(conflictValues(page, 7)).toHaveText(["またね。", "さようなら。"]);
  await expect(rowByLine(page, 6)).not.toHaveClass(/(^|\s)conflicted(\s|$)/);
  await expect(translationCell(page, 6)).toHaveText(extra.translation);

  const saving = nextSave(page);
  await keepButton(page).click();
  expect((await saving).status()).toBe(200);
  await waitForSaved(page);
  await expectFile(server, copy(hello(), extra, goodbye("さようなら。"), wonderful()));
});

// ---- 行き先の無い訳 ----

// 読み直した版に、その台詞の行が無いことがある（Export working copy のやり直しなど）。
// 載せる先が無い訳を捨てると、翻訳者の訳はどこにも残らない。画面がその訳の残っている
// 最後の場所になるので、キーと一緒に出し続け、「保存済み」とは言わない。
test("台詞の行がファイルから消えた訳は捨てず、キーと訳を出し続ける", async ({ page, server }) => {
  await openPaused(page, server);
  const external = copy(hello(), wonderful());
  await raiseConflict(page, server, L.goodbye, "さようなら。", external);

  await expect(orphansBox(page)).toBeVisible();
  await expect(page.locator("#orphans-title")).toHaveText(msg("ja", "ui.orphans_title"));
  await expect(page.locator("#orphans-list li")).toHaveText([`${keyFor(s.goodbye.source)}: さようなら。`]);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_orphans", { count: 1 }));
  // 選ばせる行は無い（載せる先が無い）ので、引き止めは出さない。
  await expect(conflictBox(page)).toBeHidden();

  // ほかの行を保存しても、行き先の無い訳は消えない。
  const saving = nextSave(page);
  await typeTranslation(page, 6, "最高！");
  await editor(page).press("Escape");
  expect((await saving).status()).toBe(200);
  await expectFile(server, copy(hello(), wonderful("最高！")));
  await expect(page.locator("#orphans-list li")).toHaveText([`${keyFor(s.goodbye.source)}: さようなら。`]);
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_orphans", { count: 1 }));
});

// ---- 空白を含む訳 ----

// drawn は要素の描かれた字（innerText）を並べる。textContent は空白を詰めずに持つので、
// 詰まって描かれていても一致してしまう。描かれた字で比べる。
function drawn(locator) {
  return locator.evaluateAll((nodes) => nodes.map((node) => node.innerText));
}

// 競合で並べる2つの訳は、どちらを残すか選ぶ材料である。空白だけが違う2つの訳が同じに
// 描かれると、選ぶ材料にならない。訳の欄と同じく空白を詰めずに出す（app.css の
// .note-value）。行き先の無い訳は、翻訳者が控える最後の写しなので、同じく詰めない。
test("競合で並べる2つの訳は、先頭の空白と続いた空白を詰めずに出す", async ({ page, server }) => {
  await openPaused(page, server);
  const theirs = "  またね。";
  const mine = "さよう  なら。";
  await raiseConflict(page, server, L.goodbye, mine, copy(hello(), goodbye(theirs), wonderful()));
  await expect(conflictBox(page)).toBeVisible();

  const values = conflictValues(page, L.goodbye);
  // 値は届いている（ここまでは通る）。
  expect(await values.evaluateAll((nodes) => nodes.map((node) => node.textContent))).toEqual([theirs, mine]);
  // 描かれた字も同じでなければならない。
  expect(await drawn(values)).toEqual([theirs, mine]);
});

test("行き先の無い訳は、先頭の空白と続いた空白を詰めずに出す", async ({ page, server }) => {
  await openPaused(page, server);
  const mine = "  さよう  なら。";
  await raiseConflict(page, server, L.goodbye, mine, copy(hello(), wonderful()));
  await expect(orphansBox(page)).toBeVisible();

  const value = page.locator("#orphans-list li .note-value");
  // 値は届いている（ここまでは通る）。
  expect(await value.evaluateAll((nodes) => nodes.map((node) => node.textContent))).toEqual([mine]);
  // 描かれた字も同じでなければならない。
  expect(await drawn(value)).toEqual([mine]);
});

// 行き先の無い訳はファイルに1つも入っていない。頁を閉じればその訳は消えるので、
// 未保存と同じく beforeunload で引き止める（app.js の冒頭、hasUnsaved）。
test("行き先の無い訳が残っているあいだは、頁を閉じようとすると引き止める", async ({ page, server }) => {
  await openPaused(page, server);
  await raiseConflict(page, server, L.goodbye, "さようなら。", copy(hello(), wonderful()));
  // 未保存は無く、残っているのは行き先の無い訳だけ。
  await expect(saveState(page)).toHaveText(msg("ja", "ui.save_orphans", { count: 1 }));

  const outcome = new Promise((resolve) => {
    page.once("dialog", async (dialog) => {
      const type = dialog.type();
      await dialog.dismiss();
      resolve(type);
    });
    page.once("close", () => resolve("closed"));
  });
  await page.close({ runBeforeUnload: true });
  expect(await outcome).toBe("beforeunload");
  expect(page.isClosed()).toBe(false);
  await expect(page.locator("#orphans-list li")).toHaveText([`${keyFor(s.goodbye.source)}: さようなら。`]);
});

// 同じキーの行が2つに増えると、どちらへ載せるかを画面では決められない
// （README「同じキーが増えた」）。当て推量でどちらかへ入れると、別の行の訳を
// 上書きしうる。決められない訳も捨てずに行き先の無い訳として出す。
test("同じキーの行が2つに増えて載せ先を決められない訳も、行き先の無い訳として出す", async ({ page, server }) => {
  await openPaused(page, server);
  const external = copy(hello(), extra, goodbye(), goodbye(), wonderful());
  await raiseConflict(page, server, L.goodbye, "さようなら。", external);

  await expect(page.locator("#orphans-list li")).toHaveText([`${keyFor(s.goodbye.source)}: さようなら。`]);
  await expect(conflictBox(page)).toBeHidden();
  // どちらの goodbye の行にも入れない。
  await expect(translationCell(page, 7)).toHaveText("");
  await expect(translationCell(page, 8)).toHaveText("");

  await page.clock.runFor(pastRetries);
  expect(await rowPosts(page)).toBe(1);
  await expectFile(server, external);
});

// ---- 続けて起きたとき・送っている最中 ----

// 1度目の競合の最中に打った訳は、2度目の 409 で「競合したもの」へ移ってはならない。
// 移ると「ファイルの訳を採る」で一緒に捨てられ、ファイルにも画面にも残らず、
// beforeunload も効かない形になった（doc.go に実測がある）。
test("409 が2度続いても、選んでいるあいだに打った訳は消えない", async ({ page, server }) => {
  await openPaused(page, server);
  await raiseConflict(page, server, L.goodbye, "さようなら。", copy(hello(), goodbye("またね。"), wonderful()));
  await expect(conflictBox(page)).toBeVisible();

  // 選んでいるあいだに、競合していない行へ打つ。
  await typeTranslation(page, L.wonderful, "すごい！");
  await editor(page).press("Escape");

  // よそがもう一度、同じ行を書き換える。そのあとで自分の訳を選ぶと、2度目の 409 になる。
  await server.writeRoot(workingRel, copy(hello(), goodbye("じゃあね。"), wonderful()));
  let saving = nextSave(page);
  await keepButton(page).click();
  expect((await saving).status()).toBe(409);

  // 2度目に選ばせるのは、よそが書き換えた行だけ。打った行は巻き込まない。
  await expect(conflictValues(page, L.goodbye)).toHaveText(["じゃあね。", "さようなら。"]);
  await expect(conflictBox(page)).toBeVisible();
  await expect(rowByLine(page, L.wonderful)).not.toHaveClass(/(^|\s)conflicted(\s|$)/);
  await expect(translationCell(page, L.wonderful)).toHaveText("すごい！");

  // ファイルの訳を採っても、打った訳は残ってファイルへ入る。
  saving = nextSave(page);
  await takeButton(page).click();
  expect((await saving).status()).toBe(200);
  await waitForSaved(page);
  await expectFile(server, copy(hello(), goodbye("じゃあね。"), wonderful("すごい！")));
});

// 送ったぶんを未保存の控えから先に消さない（flush のコメント）。送っているあいだに
// 打ち足した字は、送った値より新しい。409 のとき「あなたの訳」に並ぶのは、送った値ではなく
// 最後に打った値でなければならない。送った値を並べると、打ち足した字が黙って消える。
test("保存を送っているあいだに打ち足した字も、競合したら自分の訳として残る", async ({ page, server }) => {
  await openPaused(page, server);

  // 待ち受けへ届く手前で要求を止めておき、そのあいだに打ち足しと、よその書き換えを挟む。
  let arrived;
  const reached = new Promise((resolve) => {
    arrived = resolve;
  });
  let release;
  const held = new Promise((resolve) => {
    release = resolve;
  });
  await page.route("**/api/rows", async (route) => {
    arrived();
    await held;
    await route.continue();
  });

  try {
    await typeTranslation(page, L.goodbye, "さようなら");
    const saving = nextSave(page);
    // 自動保存で送る。入力欄は開いたまま。
    await page.clock.runFor(pastAutosave);
    await reached;
    await expect(editor(page)).toBeFocused();
    await editor(page).fill("さようなら。");
    await server.writeRoot(workingRel, copy(hello(), goodbye("またね。"), wonderful()));
    release();
    expect((await saving).status()).toBe(409);
  } finally {
    release();
  }
  await page.unroute("**/api/rows");

  await expect(conflictBox(page)).toBeVisible();
  await expect(conflictValues(page, L.goodbye)).toHaveText(["またね。", "さようなら。"]);

  const saving = nextSave(page);
  await keepButton(page).click();
  expect((await saving).status()).toBe(200);
  await waitForSaved(page);
  await expectFile(server, copy(hello(), goodbye("さようなら。"), wonderful()));
});

// 読み直しは、抱えている訳を消す。競合で抱えている自分の訳もそのうちで、黙って
// 消せば「黙って破棄もしない」が破れる。消す前に尋ね、断ったら何も変えない。
test("競合中に読み直しを押すと確かめ、断れば引き止めも自分の訳もそのまま残る", async ({ page, server }) => {
  await openPaused(page, server);
  const external = copy(hello(), goodbye("またね。"), wonderful());
  await raiseConflict(page, server, L.goodbye, "さようなら。", external);
  await expect(conflictBox(page)).toBeVisible();

  const asked = new Promise((resolve) => {
    page.once("dialog", async (dialog) => {
      const seen = { type: dialog.type(), message: dialog.message() };
      await dialog.dismiss();
      resolve(seen);
    });
  });
  await page.locator("#reload").click();
  expect(await asked).toEqual({ type: "confirm", message: msg("ja", "ui.discard_confirm") });

  await expect(conflictBox(page)).toBeVisible();
  await expect(conflictValues(page, L.goodbye)).toHaveText(["またね。", "さようなら。"]);

  const saving = nextSave(page);
  await keepButton(page).click();
  expect((await saving).status()).toBe(200);
  await waitForSaved(page);
  await expectFile(server, copy(hello(), goodbye("さようなら。"), wonderful()));
});

// ---- キーを持たない行 ----

test.describe("キーを持たない行", () => {
  const keyless = (translation) => ({ ...goodbye(translation), key: "" });
  const initial = copy(hello(), keyless(), wonderful());
  test.use({ repo: sampleRepo({ workingCopy: initial }) });

  // キー列が空の行は、よそが書き換えたかどうかを照合できない。決められない行を
  // 黙って保存し直すより、人に見せるほうが安全である（app.js の fileChanged）。
  // よそが触ったのが別の行でも選ばせ、載せ直しは行番号で行う。
  test("キーを持たない行は照合できないので、よそが別の行だけを書き換えても選ばせる", async ({ page, server }) => {
    await openPaused(page, server, initial);
    const external = copy(hello(), keyless(), wonderful("最高！"));
    await raiseConflict(page, server, L.goodbye, "さようなら。", external);

    await expect(conflictBox(page)).toBeVisible();
    await expect(conflictValues(page, L.goodbye)).toHaveText(["", "さようなら。"]);
    await expect(translationCell(page, L.wonderful)).toHaveText("最高！");

    const saving = nextSave(page);
    await keepButton(page).click();
    expect((await saving).status()).toBe(200);
    await waitForSaved(page);
    await expectFile(server, copy(hello(), keyless("さようなら。"), wonderful("最高！")));
  });
});

// ---- 狭い・低い画面 ----

// 引き止めのあいだ自動保存は止まっている。ボタンに手が届かないことは、打ち続けられるのに
// 1バイトも保存されない状態が続くことを意味する（doc.go「決断のボタンは帯の先頭に出す」）。
// 600x500 と 320x600 で、実際に押しても何も起きなかったことがある。その2つと、帯の中から
// 絞り込みの一帯を外へ出す判断のときに測った 800x600 を見る。
//
// 押せるかは elementFromPoint で見る。Playwright の click は押す前に要素を画面へ送るので、
// 帯の中でスクロールアウトしていても押せてしまい、見たいことを見られない。帯の高さ
// （--top-height）が引き止めのぶん伸びることも見る。伸びないと、焦点の入る行が帯の下へ潜る。

// topHeight は --top-height の値（px）。まだ入っていなければ NaN。
function topHeight(page) {
  return page.evaluate(() =>
    parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--top-height")),
  );
}

// bandHeight は貼り付く帯（.top）のいまの高さ（px）。
function bandHeight(page) {
  return page.evaluate(() => document.querySelector(".top").getBoundingClientRect().height);
}

// reachable は selector の要素の真ん中を押したとき、その要素（か中身）に当たるか。
function reachable(page, selector) {
  return page.evaluate((sel) => {
    const target = document.querySelector(sel);
    const box = target.getBoundingClientRect();
    const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
    return hit !== null && (hit === target || target.contains(hit));
  }, selector);
}

// expectButtonsReachable は2つのボタンがどちらも押せる位置にあり、帯の高さが before から
// 伸びて、いまの帯の高さと一致していることを確かめる。
async function expectButtonsReachable(page, before) {
  await expect.poll(() => topHeight(page)).toBeGreaterThan(before);
  await expect.poll(async () => Math.abs((await topHeight(page)) - (await bandHeight(page)))).toBeLessThan(0.5);
  await expect.poll(() => reachable(page, "#conflict-keep")).toBe(true);
  await expect.poll(() => reachable(page, "#conflict-take")).toBe(true);
}

// 狭くて低い画面（320x480、375x667 など）も見る。幅が狭いと説明の文が長く折り返し、
// ボタンが2段になる。以前は 50vh の帯の見える範囲の下へ「ファイルの訳を採る」が出て、
// elementFromPoint が当たらなかった（320x500 で帯 0〜250px、ボタン 248〜279px）。
// 帯の中を送れば届いたが、送れることは画面のどこにも出ない。ボタンは帯の下端に
// 貼り付けてある（app.css の .conflict-actions）。
for (const size of [
  { width: 800, height: 600 },
  { width: 600, height: 500 },
  { width: 320, height: 600 },
  { width: 320, height: 500 },
  { width: 320, height: 480 },
  { width: 360, height: 480 },
  { width: 375, height: 667 },
  { width: 414, height: 480 },
]) {
  test(`${size.width}x${size.height} でも、競合の引き止めのボタンは押せる`, async ({ page, server }) => {
    await page.setViewportSize(size);
    await openPaused(page, server);
    await expect.poll(() => topHeight(page)).toBeGreaterThan(0);
    const before = await topHeight(page);

    const external = copy(hello(), goodbye("またね。"), wonderful());
    await raiseConflict(page, server, L.goodbye, "さようなら。", external);
    await expect(conflictBox(page)).toBeVisible();
    await expectButtonsReachable(page, before);

    // 実際に押して効く。
    const saving = nextSave(page);
    await keepButton(page).click();
    expect((await saving).status()).toBe(200);
    await expectFile(server, copy(hello(), goodbye("さようなら。"), wonderful()));
  });

  // 行き先の無い訳が同時に出ると、帯はいちばん高くなる（800x600 で 50vh に頭打ち）。
  // それでも引き止めが先頭に出ていて押せることを見る。
  test(`${size.width}x${size.height} で行き先の無い訳と並んでも、競合の引き止めのボタンは押せる`, async ({ page, server }) => {
    await page.setViewportSize(size);
    await openPaused(page, server);
    await expect.poll(() => topHeight(page)).toBeGreaterThan(0);
    const before = await topHeight(page);

    // 1度目の競合の最中に wonderful へ打ち、2度目の 409 でその行が消え、goodbye は
    // また書き換わっている。競合（goodbye）と行き先の無い訳（wonderful）が両方出る。
    await raiseConflict(page, server, L.goodbye, "さようなら。", copy(hello(), goodbye("またね。"), wonderful()));
    await expect(conflictBox(page)).toBeVisible();
    await typeTranslation(page, L.wonderful, "すごい！");
    await editor(page).press("Escape");
    await server.writeRoot(workingRel, copy(hello(), goodbye("じゃあね。")));
    let saving = nextSave(page);
    await keepButton(page).click();
    expect((await saving).status()).toBe(409);
    await expect(conflictValues(page, L.goodbye)).toHaveText(["じゃあね。", "さようなら。"]);
    await expect(page.locator("#orphans-list li")).toHaveText([`${keyFor(s.wonderful.source)}: すごい！`]);

    await expectButtonsReachable(page, before);

    saving = nextSave(page);
    await keepButton(page).click();
    expect((await saving).status()).toBe(200);
    await expectFile(server, copy(hello(), goodbye("さようなら。")));
  });
}
