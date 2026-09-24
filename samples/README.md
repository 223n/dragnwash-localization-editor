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

`docs/images/`に8枚を書きます。  
4つの場面を、画面の言語（`ja`と`en`）ごとに撮ります。  
見本は一時ディレクトリへ写してから開くので、`samples/harbor`は変わりません。

同じ見本を撮り直しても、画像のバイトは揃わないことがあります。  
左の列の見出しの文字などが、1画素に満たない幅でずれるためです。  
目では見分けられないので、撮り直した画像は、場面の中身が変わったときだけコミットしてください。

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
