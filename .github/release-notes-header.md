## 翻訳者の方へ

お使いの環境のものを1つ落としてください。

| 環境 | 落とすもの |
| ---- | ---- |
| macOS（Apple Silicon） | `darwin_arm64.tar.gz` |
| macOS（Intel） | `darwin_amd64.tar.gz` |
| Windows（ふつうのPC） | `windows_amd64.zip` |
| Windows（arm64機） | `windows_arm64.zip` |
| Linux | `linux_amd64.tar.gz` |
| Linux（arm64機） | `linux_arm64.tar.gz` |

`darwin`はmacOSのことです。
自分がどちらか分からないときは、次の方法で確かめられます。

- macOSとLinuxは`uname -m`を実行します。`arm64`ならarm64、`x86_64`ならamd64です
- Windowsは「設定」→「システム」→「バージョン情報」の「システムの種類」を見ます

展開して実行する手順と、macOSやWindowsで止められたときの対処は[README](https://github.com/223n/dragnwash-localization-editor#入手する)にあります。

`checksums.txt`は、落とした書庫が壊れていないかを確かめるためのものです。
配布元が本物であることの証明にはなりません。
