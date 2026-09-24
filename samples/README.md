# 見本のリポジトリ

**日本語** | [English](README.en.md)

`samples/harbor`は、画面を試すための架空の翻訳リポジトリです。  
台詞と人物は、この見本のために作ったものです。  
ゲームの台本は含みません。

## 中身

| ファイル                                         | 中身                                                     |
|--------------------------------------------------|----------------------------------------------------------|
| `harbor/data/script_order.csv`                   | 再生順です。台詞は10行あります                           |
| `harbor/Translations/ja/strings.csv`             | `ja`の公開ファイルです。`publish`の出力そのものです      |
| `harbor/Translations/de/strings.csv`             | `de`の公開ファイルです。訳は2行だけです                  |
| `harbor/Translations/_discovered/ja.working.csv` | `ja`の作業コピーです。原文の欄と、訳の空いた行があります |
| `screenshots.mjs`                                | READMEの画面の例を撮り直す台本です                       |
| `compare-png.mjs`                                | 撮り直した画像が揺れだけかを、画素で見分けます           |

見本には、画面に出したいものをわざと残してあります。  
未翻訳の行は3つ、原文とタグの違う行は1つです。  
タグの違う行が要確認に当たるので、`dwloc diff --no-game`は終了コード1で終わります。  
写しで走らせると、`dwloc validate`は問題を出さず、`dwloc publish --no-game --dry-run`は変更なしで終わります。  
手元にゲームがあると、`--no-game`を付けない`diff`と`publish`はゲームの作業コピーを読み、結果が変わります。  
`samples/harbor`のまま`dwloc validate`を掛けると、終了コード1で終わります。  
作業コピーをこのリポジトリにコミットしてあり、コミットしてはいけないファイルとして数えるためです。

## 画面を試す

`edit`は保存するたびにファイルを書き換えます。  
見本そのものを変えないように、写しを開いてください。

```bash
go build ./cmd/dwloc
cp -r samples/harbor /tmp/harbor
./dwloc edit --root /tmp/harbor --no-game
```

Windowsでは、PowerShellで次のように打ちます。

```powershell
go build ./cmd/dwloc
Copy-Item -Recurse samples/harbor $env:TEMP/harbor
.\dwloc.exe edit --root $env:TEMP/harbor --no-game
```

`--no-game`を付けるのは、手元にゲームがあっても、そちらの作業コピーを読まないためです。  
付けないと、`edit`はSteamのライブラリからゲームを探します。

## 画面の例を撮り直す

READMEの画面の例（`docs/images/edit-*.png`）は、この見本で撮っています。  
画面を変えたら、次の手順で撮り直せます。

```bash
npm ci
npx playwright install chromium   # 初回だけ
npm run screenshots
```

`docs/images/`へ8枚を撮ります。  
場面は4つで、画面の言語（`ja`と`en`）ごとに1枚ずつです。  
見本は一時ディレクトリへ写してから開くので、`samples/harbor`は変わりません。

同じ見本を撮り直しても、画像は画素まで揃いません。  
表の見出しの下の線や左の列の文字が1画素上下にずれたり、角の色が1だけ違ったりします。  
そのため、書く前に同じ名前の画像（既定では`docs/images/`にあるコミット済みの画像）と画素で比べます。  
違いが揺れの範囲に収まる画像は書き換えません。  
端末には、画像ごとに書いたか残したかと、その理由を1行ずつ出します。

揺れの範囲とみなすのは、次の2つだけです。

- 各色の違いが1以内の画素
- 1画素上か下にずれただけの画素。画像全体の0.5%までです

場面の中身が変わった画像だけが書き換わるので、差分の出た画像はそのままコミットできます。  
ただし、線や文字がちょうど1画素だけ上下に動いた変化は、揺れと見分けられません。  
揺れの範囲でも撮り直した画像にしたいときは、古い画像を消してから撮ります。  
比べ方は`compare-png.mjs`にあり、E2Eの`e2e/harness/compare-png.spec.mjs`で確かめています。

## 見本を直すとき

キーは原文から作ります（`internal/key`と同じで、原文のSHA-256の先頭8バイトです）。  
原文を変えたら、再生順・公開ファイル・作業コピーのキーをそろえて直してください。  
公開ファイルは、写しで`dwloc publish --no-game`を回した出力で置き換えます。

`section`は、`L01 Harbor`の形か、英数字と`_`だけにします。  
それ以外の形は`dwloc validate`が指摘します。

リリースのワークフローは、配る書庫をこの見本の写しで動かして確かめます。  
直したあとも、写しで`dwloc validate`が問題を出さず、`dwloc publish --no-game --dry-run`が変更なしで終わる形を保ってください。  
崩れると、Goのテスト`TestSampleHarborPassesReleaseChecks`（`cmd/dwloc/samples_test.go`）が落ちます。

撮り直しの台本は、`ja.working.csv`の10行目の訳が空いていることを前提にしています。  
行を足したり並べ替えたりしたら、`screenshots.mjs`の行番号も直してください。
