// 画面の例（screenshots.mjs が撮る PNG）を画素で比べ、撮り直しの揺れだけの違いかを見分ける。
//
// 同じ見本を撮り直しても、Chromium は画像を画素まで揃えない。見た揺れは3種類ある。
//   - 表の見出しの下の線が1画素下がり、線と見出しのあいだの白い1画素の行が消える（逆もある）
//   - 左の列の文字が、1画素上か下にずれる
//   - ボタンの角などの色が、各チャンネルで1だけ違う
// 箱の位置（getBoundingClientRect）は毎回小数まで同じなので、小数の位置にある箱を
// 画素へ丸める段で揺れていると考えられる。CSS を整数の px にそろえても消えなかった。
//
// そこで撮る側で吸収する。揺れの範囲なら、コミット済みの画像をそのまま残す。
// 撮り直すたびに意味の無い画像の差分が出ると、レビューで「中身の変化か揺れか」を
// 目で見分けることになるからである。
//
// 依存を増やさないよう、PNG は node:zlib だけで読む。Chromium の書く形（8ビットの
// RGB か RGBA、飛び越し無し）と、グレースケールだけを読む。読めない形は例外にし、
// 呼び出し側は比べずに書く側へ倒す。
import { inflateSync } from "node:zlib";

// CHANNEL_TOLERANCE は、同じ画素とみなす各チャンネルの違いの上限。角の色の揺れが1だった。
export const CHANNEL_TOLERANCE = 1;

// MAX_SHIFTED_RATIO は、上下に1画素ずれただけの画素を、全体のどれだけまで揺れとみなすか。
//
// 見た揺れは、1280×800 の画像で多くて 3,624 画素（0.35%）だった。線と文字の列が
// 1本ずつずれる分である。それに少し余裕を足した。一覧全体が1画素ずれるような本物の
// 変化は、行ごとの線と文字が動くので、これを大きく超える。上限を超えた揺れは書く側に
// 倒れるので、画像の差分が出るだけで、変化を見落とすことはない。
export const MAX_SHIFTED_RATIO = 0.005;

const SIGNATURE = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);

// CHANNELS は色の種類（IHDR の color type）ごとの、1画素のバイト数。
const CHANNELS = { 0: 1, 2: 3, 4: 2, 6: 4 };

// decodePng は PNG を読み、{ width, height, data } を返す。data は RGBA の順に並べる。
export function decodePng(buffer) {
  if (buffer.length < SIGNATURE.length || !buffer.subarray(0, SIGNATURE.length).equals(SIGNATURE)) {
    throw new Error("PNG ではありません");
  }
  let header = null;
  const idat = [];
  let pos = SIGNATURE.length;
  while (pos + 8 <= buffer.length) {
    const length = buffer.readUInt32BE(pos);
    const type = buffer.toString("latin1", pos + 4, pos + 8);
    const end = pos + 8 + length;
    if (end + 4 > buffer.length) {
      throw new Error(`PNG の ${type} の塊が途中で切れています`);
    }
    const data = buffer.subarray(pos + 8, end);
    if (type === "IHDR") {
      header = {
        width: data.readUInt32BE(0),
        height: data.readUInt32BE(4),
        bitDepth: data[8],
        colorType: data[9],
        interlace: data[12],
      };
    } else if (type === "IDAT") {
      idat.push(data);
    } else if (type === "IEND") {
      break;
    }
    pos = end + 4; // CRC は読み飛ばす。壊れていれば、たいてい inflate か大きさの確かめで止まる。
  }
  if (!header) {
    throw new Error("PNG に IHDR がありません");
  }
  const { width, height, bitDepth, colorType, interlace } = header;
  const bpp = CHANNELS[colorType];
  if (bitDepth !== 8 || bpp === undefined || interlace !== 0) {
    throw new Error(`読めない形の PNG です（ビット深度 ${bitDepth}、色の種類 ${colorType}、飛び越し ${interlace}）`);
  }
  const stride = width * bpp;
  const raw = inflateSync(Buffer.concat(idat));
  if (raw.length !== height * (stride + 1)) {
    throw new Error("PNG の画素の量が大きさと合いません");
  }
  const pixels = unfilter(raw, width, height, bpp);
  return { width, height, data: toRgba(pixels, width * height, colorType) };
}

// unfilter は行ごとのフィルターを外す（PNG の仕様の 9.2 から 9.4）。
function unfilter(raw, width, height, bpp) {
  const stride = width * bpp;
  const out = new Uint8Array(height * stride);
  for (let y = 0; y < height; y++) {
    const filter = raw[y * (stride + 1)];
    const line = y * (stride + 1) + 1;
    const row = y * stride;
    for (let x = 0; x < stride; x++) {
      const a = x >= bpp ? out[row + x - bpp] : 0;
      const b = y > 0 ? out[row - stride + x] : 0;
      const c = x >= bpp && y > 0 ? out[row - stride + x - bpp] : 0;
      let predictor;
      switch (filter) {
        case 0:
          predictor = 0;
          break;
        case 1:
          predictor = a;
          break;
        case 2:
          predictor = b;
          break;
        case 3:
          predictor = (a + b) >> 1;
          break;
        case 4: {
          const p = a + b - c;
          const pa = Math.abs(p - a);
          const pb = Math.abs(p - b);
          const pc = Math.abs(p - c);
          predictor = pa <= pb && pa <= pc ? a : pb <= pc ? b : c;
          break;
        }
        default:
          throw new Error(`PNG の ${y + 1} 行目のフィルター ${filter} を知りません`);
      }
      out[row + x] = (raw[line + x] + predictor) & 0xff;
    }
  }
  return out;
}

// toRgba は画素を RGBA の並びへ広げる。比べる側が色の種類を気にしなくて済むようにする。
function toRgba(pixels, count, colorType) {
  if (colorType === 6) {
    return pixels;
  }
  const rgba = new Uint8Array(count * 4);
  for (let i = 0; i < count; i++) {
    let r;
    let g;
    let b;
    let alpha = 255;
    if (colorType === 2) {
      [r, g, b] = [pixels[i * 3], pixels[i * 3 + 1], pixels[i * 3 + 2]];
    } else if (colorType === 0) {
      r = g = b = pixels[i];
    } else {
      r = g = b = pixels[i * 2];
      alpha = pixels[i * 2 + 1];
    }
    rgba.set([r, g, b, alpha], i * 4);
  }
  return rgba;
}

// near は a の i 番目と b の j 番目の画素が、各チャンネルで許容の範囲に収まるかを返す。
function near(a, i, b, j) {
  for (let k = 0; k < 4; k++) {
    if (Math.abs(a[i + k] - b[j + k]) > CHANNEL_TOLERANCE) {
      return false;
    }
  }
  return true;
}

// matchesVerticalNeighbour は、from の (x, y) の画素が、to の1画素上か下の画素と
// 同じ（許容の範囲）かを返す。
function matchesVerticalNeighbour(from, to, x, y) {
  const i = (y * from.width + x) * 4;
  for (const dy of [-1, 1]) {
    const ny = y + dy;
    if (ny >= 0 && ny < to.height && near(from.data, i, to.data, (ny * to.width + x) * 4)) {
      return true;
    }
  }
  return false;
}

// 画素の分け方。SAME は許容の範囲で同じ、SHIFTED は上下のずれ、HALF は片向きだけ
// ずれで説明できるもの、CHANGED は違う画素。
const SAME = 0;
const SHIFTED = 1;
const HALF = 2;
const CHANGED = 3;

// nearShift は、kind の (x, y) と同じ列の2画素以内に、両向きのずれ（SHIFTED）があるかを返す。
function nearShift(kind, width, height, x, y) {
  for (const dy of [-2, -1, 1, 2]) {
    const ny = y + dy;
    if (ny >= 0 && ny < height && kind[ny * width + x] === SHIFTED) {
      return true;
    }
  }
  return false;
}

// comparePictures は2枚の画像を比べ、揺れの範囲に収まるかを返す。
//
// 違う画素（各チャンネルの違いが CHANNEL_TOLERANCE を超える画素）を、上下のずれで
// 説明できるかで分ける。
//   - 新しい画素が前の画像の1画素上か下にあり（前向き）、前の画素も新しい画像の1画素
//     上か下にある（後ろ向き）なら、上下のずれ（shifted）と数える。中身が1画素動いた形
//   - 片向きだけで説明できる画素は、同じ列の2画素以内に両向きのずれがあるときだけ、
//     ずれと数える。線が1画素下がって白い行が消える揺れでは、消える白い行（逆向きなら
//     現れる白い行）が片向きにしかならない。その2画素上に、線の動いた行がある
//   - それ以外は違う画素（changed）と数える
// 片向きの画素をいつもずれと数えると、平らな背景に高さ1〜2画素の横線や点が増えたり
// 消えたりしただけの変化まで、揺れに見えてしまう。背景の画素は、上下の背景と合うから
// である。近くに両向きのずれが無ければ、そうした変化として数える。
//
// 線や文字がちょうど1画素だけ上下に動いた変化は、揺れと見分けられない。揺れそのものが
// その形だからである。
//
// 揺れの範囲とみなすのは、大きさが同じで、changed が0で、shifted が全体の
// MAX_SHIFTED_RATIO 以下のときだけ。違う場面どうしを比べると、どの組でも changed が
// 3万を超えた。
export function comparePictures(before, after) {
  if (before.width !== after.width || before.height !== after.height) {
    return {
      jitterOnly: false,
      reason: `大きさが違います（${before.width}×${before.height} と ${after.width}×${after.height}）`,
      changed: null,
      shifted: null,
    };
  }
  const { width, height } = before;
  const kind = new Uint8Array(width * height);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const p = y * width + x;
      if (near(before.data, p * 4, after.data, p * 4)) {
        kind[p] = SAME;
        continue;
      }
      const forward = matchesVerticalNeighbour(after, before, x, y);
      const backward = matchesVerticalNeighbour(before, after, x, y);
      kind[p] = forward && backward ? SHIFTED : forward || backward ? HALF : CHANGED;
    }
  }
  let changed = 0;
  let shifted = 0;
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      let k = kind[y * width + x];
      if (k === HALF) {
        k = nearShift(kind, width, height, x, y) ? SHIFTED : CHANGED;
      }
      if (k === SHIFTED) {
        shifted++;
      } else if (k === CHANGED) {
        changed++;
      }
    }
  }
  const limit = Math.floor(width * height * MAX_SHIFTED_RATIO);
  let reason;
  if (changed > 0) {
    reason = `違う画素が ${changed} 個あります`;
  } else if (shifted > limit) {
    reason = `上下に1画素ずれた画素が ${shifted} 個あり、揺れとみなす上限（${limit} 個）を超えます`;
  } else if (shifted > 0) {
    reason = `上下に1画素ずれた画素が ${shifted} 個だけです`;
  } else {
    reason = "各チャンネルの違いが1以内です";
  }
  return { jitterOnly: changed === 0 && shifted <= limit, reason, changed, shifted };
}

// judgeRetake は、撮り直した画像 taken を書くべきかを決める。
//
// existing は同じ名前で既にある画像のバイト列で、無ければ null。書くべきなら
// write が true になる。既にある画像を読めないときも書く側へ倒す（比べられないのに
// 残すと、壊れた画像が居座る）。reason は端末に出す短い理由。
export function judgeRetake(existing, taken) {
  if (existing === null) {
    return { write: true, reason: "新しい画像です" };
  }
  if (Buffer.compare(existing, taken) === 0) {
    return { write: false, reason: "バイトまで同じです" };
  }
  let before;
  let after;
  try {
    before = decodePng(existing);
    after = decodePng(taken);
  } catch (err) {
    return { write: true, reason: `画素で比べられません（${err.message}）` };
  }
  const result = comparePictures(before, after);
  return { write: !result.jitterOnly, reason: result.reason };
}
