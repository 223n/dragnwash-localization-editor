## 翻訳者の方へ／For translators

お使いの環境のものを1つ落としてください。  
Download the one file that matches your environment.

| 環境／Environment | 落とすもの／What to download |
| ---- | ---- |
| macOS（Apple Silicon） | `darwin_arm64.tar.gz` |
| macOS（Intel） | `darwin_amd64.tar.gz` |
| Windows（ふつうのPC／ordinary PC） | `windows_amd64.zip` |
| Windows（arm64機／arm64 machine） | `windows_arm64.zip` |
| Linux | `linux_amd64.tar.gz` |
| Linux（arm64機／arm64 machine） | `linux_arm64.tar.gz` |

`darwin`はmacOSのことです。  
`darwin` means macOS.

自分がどちらか分からないときは、次の方法で確かめられます。  
If you are not sure which one you are on, you can check it as follows.

- macOSとLinuxは`uname -m`を実行します。`arm64`ならarm64、`x86_64`ならamd64です  
  On macOS and Linux, run `uname -m`. `arm64` means arm64 and `x86_64` means amd64
- Windowsは「設定」→「システム」→「バージョン情報」の「システムの種類」を見ます  
  On Windows, look at "System type" under "Settings" → "System" → "About"

展開して実行する手順と、macOSやWindowsで止められたときの対処は[README](https://github.com/223n/dragnwash-localization-editor#入手する)にあります。  
The steps to extract and run it are in the [README](https://github.com/223n/dragnwash-localization-editor/blob/main/README.en.md#download).

`checksums.txt`は、落とした書庫が壊れていないかを確かめるためのものです。  
配布元が本物であることの証明にはなりません。

`checksums.txt` checks that the archive you downloaded is not corrupted.  
It is not proof that the distributor is genuine.
