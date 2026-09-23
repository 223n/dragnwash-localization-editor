// 画面を操作する小さな道具。セレクターを試験ごとに書き散らさないためにある。
//
// 画面の骨組みは internal/web/ui/index.html、行の組み立ては app.js の rowNode にある。
// 行は div の並びで、訳の欄（.cell.translation）は編集できる行にだけ data-line を持つ。
// 入力欄は頁に1つだけで、焦点が入った行へ差し込まれる（textarea.editor）。
import { expect } from "@playwright/test";

// openApp はトークン付きの URL を開き、画面が組み上がるまで待つ。
//
// 待つのは2つ。目録が届いて保存の状態が描かれたこと、ロケールを指定して起動した
// ときは行が描かれたこと（#rows は render のあとでしか埋まらない）。
export async function openApp(page, server) {
  await page.goto(server.url);
  await expect(page.locator("#save-state")).not.toBeEmpty();
  if (server.options.locale) {
    await expect(page.locator("#rows")).not.toBeEmpty();
  }
}

// dataRows は一覧のデータ行（見出しを除く）。
export function dataRows(page) {
  return page.locator("#list .row");
}

// headings は一覧の見出し。
export function headings(page) {
  return page.locator("#list .heading");
}

// rowByLine は物理行番号 n の行。編集できない行も引ける（番号の欄で引く）。
export function rowByLine(page, n) {
  return page.locator("#list .row").filter({
    has: page.locator(".cell.num", { hasText: new RegExp(`^${n}$`) }),
  });
}

// translationCell は物理行番号 n の訳の欄（編集できる行だけ）。
export function translationCell(page, n) {
  return page.locator(`#list .cell.translation[data-line="${n}"]`);
}

// editor は差し込まれている入力欄。
export function editor(page) {
  return page.locator("#list textarea.editor");
}

// saveState は上の帯の保存の状態。
export function saveState(page) {
  return page.locator("#save-state");
}

// openEditor は行 n の訳の欄を押して入力欄を開く。
export async function openEditor(page, n) {
  await translationCell(page, n).click();
  await expect(editor(page)).toBeFocused();
}

// typeTranslation は行 n の入力欄を開き、訳を value に置き換える。
//
// fill は入力欄の中身を選んでから差し替え、input を1回だけ投げる。1字ずつ打つ
// （pressSequentially）と打鍵ごとに自動保存の時計が引き直される。どちらを使うかは
// 見たい振る舞いで決めること。閉じない（blur しない）ので、保存は自動保存の
// 時計か、呼び出し側の操作（Enter、Tab、ほかの場所を押す）で走る。
export async function typeTranslation(page, n, value) {
  await openEditor(page, n);
  await editor(page).fill(value);
}

// waitForSaved は保存の状態が「保存済み」になるまで待つ。
//
// 入力の直後は「未保存」、送っているあいだは「保存しています」になるので、
// 入力のあとで呼べば、その保存が返るまで待つことになる。
export async function waitForSaved(page) {
  await expect(saveState(page)).toHaveClass(/(^|\s)clean(\s|$)/);
}
