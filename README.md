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

`CLI`の`dwloc`が動きます。
既存のPowerShellとPythonのスクリプトを置き換えられます。
編集用の`UI`はまだありません。

| できること | 元のツール |
| ---- | ---- |
| `dwloc publish` | `tools/hash-strings.ps1` |
| `dwloc validate` | `tools/check-translations.py` |
| `dwloc diff` | 対応するものはありません |

移植が正しいことは、元リポジトリの`Translations/<locale>/strings.csv`を入力にして`dwloc publish`を通し、入力とバイト単位で一致するかで確かめます。
13ロケールすべてで一致します。
`validate`は元のPythonスクリプトと同じ報告を出すかで確かめます。

| 文書 | 内容 |
| ---- | ---- |
| [docs/research.md](docs/research.md) | 事前調査レポートです。既存仕様の分析、`UI`方式の比較、推奨する構成があります |
| [docs/port-spec.md](docs/port-spec.md) | 既存実装から抽出した移植仕様です。156件の規則と、敵対的な検証で見つかった42件の食い違いが入っています |

## 入手する

[Releases](https://github.com/223n/dragnwash-localization-editor/releases)から、お使いの環境向けの書庫をダウンロードします。
Goのインストールは要りません。

### どのファイルを落とすか

ファイル名は`dwloc_<版>_<OS>_<CPU>`の形です。

| 使っている環境 | 落とすファイル |
| ---- | ---- |
| Windows（ふつうのPC） | `dwloc_<版>_windows_amd64.zip` |
| Windows（Snapdragonなどのarm64機） | `dwloc_<版>_windows_arm64.zip` |
| macOS（M1以降のApple Silicon） | `dwloc_<版>_darwin_arm64.tar.gz` |
| macOS（Intel） | `dwloc_<版>_darwin_amd64.tar.gz` |
| Linux（64ビットのPC） | `dwloc_<版>_linux_amd64.tar.gz` |
| Linux（Raspberry Piなどのarm64機） | `dwloc_<版>_linux_arm64.tar.gz` |

`darwin`はmacOSのことです。
`amd64`はIntelとAMDの64ビットのCPU、`arm64`はApple SiliconやSnapdragonなどのCPUを指します。

どちらか分からないときは、その場で確かめられます。

| 環境 | 確かめかた | 見かた |
| ---- | ---- | ---- |
| macOS | ターミナルで`uname -m`を実行します | `arm64`ならarm64、`x86_64`ならamd64です |
| Linux | 端末で`uname -m`を実行します | `aarch64`ならarm64、`x86_64`ならamd64です |
| Windows | 「設定」→「システム」→「バージョン情報」の「システムの種類」を見ます | 「ARMベース」と書いてあればarm64、そうでなければamd64です |

間違えたものを落とすと、実行したときに起動しません。
落とし直せば直ります。
自動で付く「Source code」の2つはソースコードです。
バイナリは入っていません。

書庫の中身は3つです。

| 中身 | 何か |
| ---- | ---- |
| `dwloc`（Windowsは`dwloc.exe`） | 本体です |
| `LICENSE` | ライセンス（Apache License 2.0）です |
| `README.txt` | 実行のしかたを短くまとめたものです |

このバイナリには署名を付けていません。
署名にはApple Developer Program（年99ドル）とWindowsの証明書が要るため、今のところ見送っています。
そのため、macOSとWindowsでは初回に警告の出る場合があります。
下に回避の手順を書きます。

### Windowsで実行する

1. ダウンロードしたzipを右クリックし、「すべて展開」を選びます
1. PowerShellを開き、展開したフォルダーへ移動します
1. `.\dwloc.exe version`を実行します。版が表示されれば動いています

「WindowsによってPCが保護されました」と出た場合は、「詳細情報」を押してから「実行」を選びます。
これはSmartScreenによる、署名の無いプログラムへの警告です。

展開したフォルダーに`dwloc.exe`が見当たらない場合は、Microsoft Defenderによる隔離を疑ってください。
何が隔離されたかは、Windowsセキュリティの「保護の履歴」で確認できます。
元のリポジトリのREADMEには、配布物の`Install.exe`を`Trojan:Script/Wacatac.B!ml`として検出した事例が書かれています。
そこでは、これは誤検知であり、末尾の`!ml`は機械学習による推定を表すと説明されています。
署名の無いファイルは、このように検出されることがあります。
隔離された場合は、まず下の「チェックサムを確かめる」で、**落とした`zip`が**配布物と同じかを確かめてください。
公開しているチェックサムは書庫に対するもので、`dwloc.exe`単体のハッシュは載せていません。
書庫が配布物と同じであれば、そこから出てきた`dwloc.exe`も同じものです。
そのうえで「保護の履歴」から許可します。
許可するのは、そのファイルだけにします。

> [!NOTE]
> Windowsでの実際の見え方は、このバイナリでは確かめていません。
> 上の内容は[docs/research.md](docs/research.md)の5.3節と、元のリポジトリのREADMEの記述によるものです。

### macOSで実行する

ターミナルを開き、ダウンロードしたフォルダーで次を実行します。

```bash
tar xzf dwloc_<版>_darwin_arm64.tar.gz
cd dwloc_<版>_darwin_arm64
xattr -d com.apple.quarantine ./dwloc
./dwloc version
```

`xattr`の行は、ダウンロードしたファイルに付く隔離の印を外します。
この印が残っていると、Gatekeeperが実行を止めます。
`No such xattr`と出た場合は、印が付いていないので、そのまま次へ進みます。

[docs/research.md](docs/research.md)の5.3節には、macOS 15のSequoia以降で右クリックからの「開く」による回避が廃止されたと書かれています。
同じ節に、コマンドラインのバイナリであれば`xattr`の1行で済むとあります。

> [!NOTE]
> macOSでの実際の見え方も、このバイナリでは確かめていません。

### Linuxで実行する

```bash
tar xzf dwloc_<版>_linux_amd64.tar.gz
cd dwloc_<版>_linux_amd64
./dwloc version
```

書庫の中の`dwloc`には実行権限を付けてあります。
`Permission denied`と出る場合は、展開のしかたで権限が落ちています。
`chmod +x ./dwloc`を実行してください。

Linuxには署名の仕組みが事実上ありません（[docs/research.md](docs/research.md)の5.3節）。
実行権限だけで動きます。

### チェックサムを確かめる

`dwloc_<版>_checksums.txt`に、6つの書庫それぞれのSHA-256を載せています。
1行は「ハッシュ、スペース2つ、ファイル名」の形です。
落とした書庫と同じフォルダーに置いて確かめます。

macOSとLinuxでは、次のように確かめます。

```bash
sha256sum --check --ignore-missing dwloc_<版>_checksums.txt      # Linux
shasum -a 256 --check --ignore-missing dwloc_<版>_checksums.txt  # macOS
```

`--ignore-missing`は、手元にある書庫だけを対象にします。
これを付けないと、落としていない5つが見つからずに失敗します。

WindowsではPowerShellで確かめます。

```powershell
Get-FileHash -Algorithm SHA256 .\dwloc_<版>_windows_amd64.zip
```

表示された`Hash`と、`checksums.txt`の同じファイル名の行が一致すれば、配布しているものと同じファイルです。
大文字と小文字の違いは無視してかまいません。
一致は「配布しているファイルと同じもの」の確認であって、安全であることの証明ではありません。

リリースをやり直すと、書庫に入る時刻が変わるため、チェックサムも変わります。
書庫とチェックサムは、必ず同じReleaseのものを使ってください。

## 使い方

ダウンロードした`dwloc`を、翻訳リポジトリのルートを指して実行します。
自分でビルドする場合は、下の「開発」を見てください。

```bash
./dwloc validate --root ../dragnwash-localization
./dwloc diff     --root ../dragnwash-localization
./dwloc publish  --root ../dragnwash-localization --dry-run
./dwloc publish  --root ../dragnwash-localization
```

| サブコマンド | 何をするか |
| ---- | ---- |
| `validate` | `Translations/<locale>/strings.csv`の形式を検査します |
| `diff` | 次にやることと、確かめたほうがよい行を並べます |
| `publish` | 公開用の`strings.csv`を作り直します |
| `edit` | 手元だけの待ち受けを始め、ブラウザーで訳を書き換えます |
| `version` | 版を表示します |

### 画面で訳を書き換える

`edit`は、手元だけの待ち受けを始めます。
`127.0.0.1`にしか束ねません。

```bash
./dwloc edit --root ../dragnwash-localization --locale ja
```

標準出力に出た`URL`をブラウザーで開くと、1ロケールの全行が並びます。
訳の欄を選ぶと書き換えられます。
入力が止まると自動で保存します。

画面での操作は次のとおりです。

| 操作 | 何が起きるか |
| ---- | ---- |
| 絞り込み | 選んだ条件のどれかに当たった行だけを出します。複数選べます |
| 検索 | speaker、原文、訳、キーに打った字を含む行だけを出します |
| 条件を外す | 絞り込みと検索をまとめて外します |
| `Enter` | 訳を確定して、次の（いま出ている）行の入力欄を開きます。出ている最後の行では、閉じずにその行に留まります |
| `Escape` | 入力欄を閉じます。打った訳は残ります |
| `Tab` | 次の行へ移ります |
| `/` | 検索の欄へ移ります。絞り込みの一帯を画面へ送ってから移ります |

絞り込みと検索の一帯は、画面の上に貼り付きません。
行の途中から触りたいときは`/`を押してください。
貼り付けると、競合したときの決断用のボタンが帯の中で押し出されて押せなくなるためです。

いま何行出ているかは、いちばん上の帯に「表示中」の行数として出ます。
条件や検索に1行も当たらなかったときは、一覧の場所にそう出ます。

`/`は、訳や検索の欄に文字を打っている間は効きません。
絞り込みのチェックボックスに焦点があるときは効きます。

検索はブラウザーの中だけで行います。
打った語は待ち受けへ送られません。
`URL`にも残りません。

ロケールを切り替えると、条件と検索語は外れます。
前のロケールで決めた条件を持ち越しません。
外れるのは読み込めたときだけです。
読み込みに失敗したときは、条件・検索欄・一覧のどれも前のままにします。

競合（手前でファイルが変わったとき）が起きている行は、どちらを残すか選ぶまで書き換えられません。
その行の訳欄を押しても入力欄は開かず、行に「どちらを残すか選んでから直せます」と出ます。
決まっていない行への入力は、`自分の訳を上に載せる`と`ファイルの訳を採る`のどちらの意味にも取れてしまうためです。
競合していない行は、引き止めが出ているあいだも今までどおり書き換えられます。

未保存の訳がある行と、保存できなかった行と、いま入力欄が開いている行は、条件に当たらなくても隠しません。
隠すと、直すべき行と触っている行が画面から消えるためです。

入力欄を閉じると、その行が条件に当たらないときはそこで隠れます。
上から順に`Enter`で打っていくときは隠れません。
閉じる時点ではまだ未保存なので、「未保存の訳がある行」として残るためです。

件数の欄の数と、条件で出る行数は一致しないことがあります。
その行がこのロケールのファイルに無いなど、行として出せないカテゴリがあるためです。

変換中（`IME`で文字を組み立てている間）は、キーを横取りしません。
変換を確定した直後の`Enter`も、行送りには使いません。
確定しただけで次の行へ飛ばないようにするためです。

### 走らせる順番

ゲームが更新されたときは、`diff`を`publish`より先に走らせてください。

```text
ゲーム更新 → ゲーム内で Export game flow → dwloc diff → 訳を直す → dwloc publish → dwloc validate
```

`diff`は、英文が変わってキーが変わった行の「引き継ぎ先」を示します。
判断の材料に、gitに残っている1つ前の`data/script_order.csv`を使います。
再生順の更新をコミットする前に走らせると、`HEAD`からそのまま読めます。

`diff`が示すのは候補であって確証ではありません。
訳は書き換えないので、中身を確かめてから移してください。

`publish`は対象をすべて組み立ててから書き出します。
1件でも失敗すれば何も書きません。
書き出しは一時ファイル経由なので、途中で止まっても元のファイルは残ります。

`--locale`で対象を絞れます。
`--path`を使うと`Translations`の走査をやめて、指定したファイルだけを変換します。

終了コードは、0が成功、1が`validate`で問題を見つけたとき、2が実行時のエラーです。

## 方針

事前調査の結論です。
決定ではなく、現時点での見通しです。

- Goの単一バイナリが、ローカルの`HTTP`サーバーを起動します。`UI`には既定のブラウザーを使います
- `IME`とRTLの扱いをブラウザーへ委ねます。Goのネイティブ描画系`GUI`ライブラリは、この2点が未解決のためです
- `CGO`に依存しません。クロスコンパイルだけで、全プラットフォーム向けのバイナリを作れます
- まず`CLI`から作ります。既存のPowerShellとPythonのスクリプトを置き換えるだけでも、独立した価値があります

`CLI`の部分はできました。
残りは`UI`です。

## 関連するリポジトリ

| リポジトリ | 関係 |
| ---- | ---- |
| [TomXV/dragnwash-localization](https://github.com/TomXV/dragnwash-localization) | 翻訳データとゲーム内プラグインの本体です。このエディターが扱う対象です |

## 開発

### 要るもの

| 道具 | 用途 |
| ---- | ---- |
| Node 22以上 | 日本語の文書の検査に使います |
| Go 1.27.1以上 | 実装に使います。`go.mod`で指定しています |

### 自分でビルドする

```bash
go build ./cmd/dwloc
```

版を埋め込む場合は、リリースと同じ形で指定します。

```bash
go build -trimpath -ldflags "-s -w -X main.version=1.2.3" ./cmd/dwloc
```

指定しない場合、`dwloc version`は`dev`と表示します。

### Goのコードを検査する

```bash
gofmt -l ./cmd ./internal   # 書式が崩れているファイルを並べる
go vet ./cmd/... ./internal/...
go test ./cmd/... ./internal/... -count=1
```

元リポジトリを参照するテストがあります。
`DRAGNWASH_SOURCE_REPO`にそのパスを入れると走ります。
指定しない場合は既定の場所を探し、見つからなければ飛ばします。

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
1. 「リリースを公開する」ワークフローが動きます。タグ`vX.Y.Z`を打ち、6種類の書庫を添えたGitHub Releaseを作り、`main`を`develop`に戻します

バイナリはタグを打つ前に作ります。
1つでもビルドに失敗した場合は、タグとGitHub Releaseを作らずに止まります。
6種類のうち一部だけが載ったReleaseを出さないためです。

添付するのは、6つの書庫と`dwloc_<版>_checksums.txt`の7ファイルです。
入れ直しのために再実行した場合は、同じ名前の添付を入れ替えます。

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
| `ci.yml` | `main`と`develop`への`push`、Pull Request、手動 | 日本語の文書、Goの書式とテスト、ワークフローの構文と安全性を検査します |
| `codeql.yml` | `main`と`develop`への`push`、Pull Request、毎週月曜、手動 | ワークフローの安全性をCodeQLで走査します |
| `labels.yml` | `.github/labels.yml`の変更、手動 | リポジトリのラベルを定義に揃えます |
| `labeler.yml` | Pull Requestを開いたとき、更新したとき | 変えたファイルとブランチ名からラベルを付けます |
| `branch-guard.yml` | Pull Requestを開いたとき、更新したとき | headブランチが`main`か`develop`なら失敗します。マージは止めません |
| `release.yml` | 手動 | `develop`からリリースブランチを切り、版を上げ、`main`へのPull Requestを開きます |
| `release-publish.yml` | `release/*`か`hotfix/*`のPull Requestが`main`にマージされたとき | 6種類のバイナリを作り、タグを打ち、GitHub Releaseを作って書庫を添付し、`main`を`develop`に戻します |

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
