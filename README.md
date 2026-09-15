# dragnwash-localization-editor

[Drag'n Wash Localization](https://github.com/TomXV/dragnwash-localization)の翻訳作業を助けるエディターです。

Windows、macOS、Linuxで動く単一のバイナリを目指します。
Go言語で実装します。

## 何をするもの

翻訳者が行う次の作業を、1つの道具にまとめます。

| 機能 | 内容 |
| ---- | ---- |
| 翻訳CSVの編集 | 原文と訳文を並べて表示し、訳文を編集します |
| 作業用ファイルとの比較 | ゲームから取り出したファイルと突き合わせます |
| 未登録の行の抽出 | 未翻訳の行と、ゲーム側に無くなった行を見つけます |
| 保存 | 公開用のCSVを生成し、形式を検証します |

## なぜ専用の道具が要るのか

元のリポジトリの`Translations/<locale>/strings.csv`は、一般的なCSVとして扱えません。
理由は3つあります。

1. `#`で始まるコメント行と空行が混ざります。Excelなどで開いて保存すると、見出しが失われます
1. 保存は書き戻しではありません。行の並び順と見出しを、ゲームの台本の順に生成し直す必要があります
1. 既存のツールはWindows専用のPowerShellと、Pythonに依存するスクリプトです。macOSやLinuxの翻訳者は実行できません

くわしくは[事前調査レポート](docs/research.md)にあります。

## 現在の状態

調査の段階です。
実装はまだ始まっていません。

| 文書 | 内容 |
| ---- | ---- |
| [docs/research.md](docs/research.md) | 事前調査レポートです。既存仕様の分析、`UI`方式の比較、推奨する構成があります |

## 方針

事前調査の結論です。
決定ではなく、現時点での見通しです。

- Goの単一バイナリが、ローカルの`HTTP`サーバーを起動します。`UI`には既定のブラウザーを使います
- `IME`とRTLの扱いをブラウザーへ委ねます。Goのネイティブ描画系`GUI`ライブラリは、この2点が未解決のためです
- `CGO`に依存しません。クロスコンパイルだけで、全プラットフォーム向けのバイナリを作れます
- まず`CLI`から作ります。既存のPowerShellとPythonのスクリプトを置き換えるだけでも、独立した価値があります

## 関連するリポジトリ

| リポジトリ | 関係 |
| ---- | ---- |
| [TomXV/dragnwash-localization](https://github.com/TomXV/dragnwash-localization) | 翻訳データとゲーム内プラグインの本体です。このエディターが扱う対象です |

## 開発

### 要るもの

| 道具 | 用途 |
| ---- | ---- |
| Node 22以上 | 日本語の文書の検査に使います |
| Go 1.21以上 | 実装を始めてから使います |

### 日本語の文書を検査する

Markdownの書式を`markdownlint`で、日本語の書き方を`textlint`で検査します。
規則は公開されている共有設定[@223n/lint-config-ja](https://www.npmjs.com/package/@223n/lint-config-ja)にあります。
このリポジトリには、何を検査するかだけを書いてあります。

```bash
npm ci
npm run lint          # 書式と日本語をまとめて検査する
npm run lint:md:fix   # 書式の指摘を直す
npm run lint:ja:fix   # 日本語の指摘のうち、機械的に直せるものを直す
```

文体は「ですます調」です。
一文一行で書きます。
全角文字と半角文字の間にスペースを入れません。

`main`と`develop`への`push`、およびすべてのPull RequestでCIが同じ検査をします。
CIではあわせて、ワークフローの構文を`actionlint`で、安全性を`zizmor`で検査します。

## 使ううえでの注意

| 場面 | 何が起きるか | どうするか |
| ---- | ---- | ---- |
| ブランチ名 | `release/`、`hotfix/`、`merge/`で始めると、リリースの仕組みが反応します | 作業ブランチには`feature/`を使います |
| Pull Requestのhead | `main`や`develop`をheadにすると、「PRのheadブランチを確かめる」が失敗します | リリースはワークフローに任せます。詳しくは[CLAUDE.md](CLAUDE.md)にあります |
| マージの方法 | squashやrebaseだと、リリースノートにPull Requestが載らず、次の版で衝突します | マージコミット（Create a merge commit）でマージします |
| 改行コード | `.gitattributes`が全ファイルをLFに固定します | CRLFのファイルを持ち込むと、最初のコミットで全行が差分になります |

## ブランチとリリース

GitFlowに沿って運用します。
ブランチの役割は[CONTRIBUTING.md](CONTRIBUTING.md)にあります。

```text
develop ──▶ release/vX.Y.Z ──(Pull Request)──▶ main ──▶ タグ vX.Y.Z と GitHub Release ──▶ develop へ戻す
```

### リリースする

1. Actionsの「リリース」を開き、「Run workflow」を選びます
1. `version`にリリースする版を入れます。`v`は付けません（例: `1.2.0`、`1.2.0-rc.1`）
1. ワークフローが`develop`から`release/vX.Y.Z`ブランチを切り、`package.json`の版を上げ、`main`へのPull Requestを開きます
1. Pull Requestの内容を確かめ、マージコミット（Create a merge commit）でマージします
1. 「リリースを公開する」ワークフローが動き、タグ`vX.Y.Z`を打ち、GitHub Releaseを作り、`main`を`develop`に戻します

版は`package.json`の`version`で管理します。
`develop`と`main`の版、最新のタグのどれよりも大きい版だけを受け付けます。
すでにあるタグや、開いたままの`release/*`ブランチがあると止まります。
`-rc.1`のようなプレリリースの版は、GitHub Releaseでもプレリリースになります。

`develop`にPull Requestを必須にする規則がある場合、`main`から`develop`への戻しは毎回Pull Requestになります。
ブランチ名は`merge/vX.Y.Z-into-develop`です。
リリースのあとに、このPull Requestもマージコミットでマージしてください。

GitHub Releaseの本文は、マージしたPull Requestのタイトルとラベルから自動で作られます。
分類は`.github/release.yml`にあります。

### 緊急の修正

リリース済みの内容を急いで直すときは、`main`から`hotfix/名前`ブランチを切ります。
そのブランチで修正し、`package.json`の版も上げます。

```bash
npm version patch --no-git-tag-version
```

`main`へのPull Requestをマージコミットでマージすると、「リリースを公開する」ワークフローが`release/*`と同じように動きます。
版を上げ忘れると、同じ版のタグがすでにあるため止まります。

## ワークフローの一覧

| ファイル | いつ動くか | 何をするか |
| ---- | ---- | ---- |
| `ci.yml` | `main`と`develop`への`push`、Pull Request、手動 | 日本語の文書、ワークフローの構文、ワークフローの安全性を検査します |
| `codeql.yml` | `main`と`develop`への`push`、Pull Request、毎週月曜、手動 | ワークフローの安全性をCodeQLで走査します |
| `labels.yml` | `.github/labels.yml`の変更、手動 | リポジトリのラベルを定義に揃えます |
| `labeler.yml` | Pull Requestを開いたとき、更新したとき | 変えたファイルとブランチ名からラベルを付けます |
| `branch-guard.yml` | Pull Requestを開いたとき、更新したとき | headブランチが`main`か`develop`なら失敗します。マージは止めません |
| `release.yml` | 手動 | `develop`からリリースブランチを切り、版を上げ、`main`へのPull Requestを開きます |
| `release-publish.yml` | `release/*`か`hotfix/*`のPull Requestが`main`にマージされたとき | タグを打ち、GitHub Releaseを作り、`main`を`develop`に戻します |

## ラベル

IssueとPull Requestのラベルはすべて日本語です。
`.github/labels.yml`が定義で、「ラベルを同期する」ワークフローがリポジトリのラベルをこの内容に揃えます。
ラベルを足したり変えたりするときは、GitHubの画面ではなくこのファイルを変えてください。

## 貢献

変更の進め方は[CONTRIBUTING.md](CONTRIBUTING.md)にあります。
`develop`から`feature/*`ブランチを切り、`develop`へのPull Requestを開きます。

## ライセンス

Apache License 2.0です。
[LICENSE](LICENSE)を見てください。
