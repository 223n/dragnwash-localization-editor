# Sample repository

**[日本語](README.md)** | English

`samples/harbor` is a made-up translation repository for trying out the screen.  
The lines and the characters were written for this sample.  
It contains none of the game's script.

## Contents

| File | Contents |
| ---- | ---- |
| `harbor/data/script_order.csv` | The play order. There are 10 lines |
| `harbor/Translations/ja/strings.csv` | The published file for `ja`. It is exactly what `publish` writes |
| `harbor/Translations/de/strings.csv` | The published file for `de`. Only 2 lines are translated |
| `harbor/Translations/_discovered/ja.working.csv` | The working copy for `ja`. It has the source column and some untranslated lines |
| `screenshots.mjs` | The script that retakes the screenshots in the README |
| `compare-png.mjs` | Tells, pixel by pixel, whether a retaken image differs only by jitter |

The sample deliberately keeps things worth showing on the screen.  
There are 3 untranslated lines and 1 line whose tags differ from the source.  
The line whose tags differ counts as "needs checking", so `dwloc diff --no-game` ends with exit code 1.  
Run on a copy, `dwloc validate` reports no problems, and `dwloc publish --no-game --dry-run` ends with no changes.  
If the game is installed, `diff` and `publish` without `--no-game` read the game's working copy, and the results change.  
Running `dwloc validate` on `samples/harbor` in place ends with exit code 1.  
The working copy is committed to this repository, and it is counted as a file that must not be committed.

## Trying the screen

`edit` rewrites the file every time it saves.  
Open a copy so that the sample itself does not change.

```bash
go build ./cmd/dwloc
cp -r samples/harbor /tmp/harbor
./dwloc edit --root /tmp/harbor --no-game
```

On Windows, type the following in PowerShell.

```powershell
go build ./cmd/dwloc
Copy-Item -Recurse samples/harbor $env:TEMP/harbor
.\dwloc.exe edit --root $env:TEMP/harbor --no-game
```

`--no-game` keeps it from reading the game's working copy, even if the game is installed.  
Without it, `edit` looks for the game in your Steam libraries.

## Retaking the screenshots

The screenshots in the README (`docs/images/edit-*.png`) are taken with this sample.  
When the screen changes, you can retake them as follows.

```bash
npm ci
npx playwright install chromium   # first time only
npm run screenshots
```

It takes 8 images for `docs/images/`.  
That is 4 scenes, one image each per screen language (`ja` and `en`).  
It opens a copy of the sample in a temporary directory, so `samples/harbor` does not change.

Retaking the same sample does not give pixel-identical images.  
The line under the table header and the text in the left column can move up or down by one pixel, and the colour of a corner can differ by 1.  
So before writing, the script compares each image pixel by pixel with the image of the same name that is already there (by default, the committed image in `docs/images/`).  
Images whose differences stay within that jitter are not rewritten.  
For each image, the terminal shows one line saying whether it was written or kept, and why.

Only these two count as jitter.

- Pixels whose colour differs by at most 1 in each channel
- Pixels that only moved up or down by one pixel, up to 0.5% of the image

Only images whose scene content changed are rewritten, so you can commit the images that show a difference as they are.  
However, a change where a line or text moves up or down by exactly one pixel cannot be told apart from jitter.  
To replace an image even when it differs only by jitter, delete the old image before retaking.  
The comparison lives in `compare-png.mjs` and is tested by the E2E spec `e2e/harness/compare-png.spec.mjs`.

## Changing the sample

Keys are made from the source text (the same as `internal/key`: the first 8 bytes of the SHA-256 of the source).  
If you change a source line, fix the key in the play order, the published files and the working copy together.  
Replace the published files with what `dwloc publish --no-game` writes when run on a copy.

Keep `section` in the form `L01 Harbor`, or to letters, digits and `_` only.  
`dwloc validate` reports any other form.

The release workflow runs the archives it ships on a copy of this sample.  
After a change, keep it so that, on a copy, `dwloc validate` reports no problems and `dwloc publish --no-game --dry-run` ends with no changes.  
If that breaks, the Go test `TestSampleHarborPassesReleaseChecks` (`cmd/dwloc/samples_test.go`) fails.

The retake script assumes that line 10 of `ja.working.csv` has an empty translation.  
If you add or reorder lines, fix the line number in `screenshots.mjs` as well.
