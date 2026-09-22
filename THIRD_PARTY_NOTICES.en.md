# Third-party works

**[日本語](THIRD_PARTY_NOTICES.md)** | English

This is the list of third-party works contained in this repository and in what it distributes.

## Font Awesome Free (icons)

The icons on the `dwloc edit` screen are copies of the shapes (`path`) only, taken from the SVG of Font Awesome Free 7.3.1.
They are embedded in the `svg` element at the top of `internal/web/ui/index.html`.

- Copyright: Copyright 2026 Fonticons, Inc. (<https://fontawesome.com>)
- License: CC BY 4.0 (<https://creativecommons.org/licenses/by/4.0/>)
- Full license text of Font Awesome Free: <https://fontawesome.com/license/free>

The icons that were copied are as follows.

```text
bars, language, globe, rotate-right, list-ol, eye, circle-check, spinner,
floppy-disk, triangle-exclamation, code-merge, link-slash, user-pen,
file-import, filter, xmark, magnifying-glass, filter-circle-xmark, keyboard,
circle-info, chevron-down, file-lines, list-check, chart-simple, hashtag, lock
```

The `xmlns` attribute and the license comment that were in the original SVG have been dropped, because of the rule that no URL may appear in the assets of the screen (`TestAssetsHaveNoExternalReference`).
Attribution is given in this file instead.
The shapes themselves are unchanged.

The Font Awesome font files (SIL OFL 1.1) and code (MIT) are not used.
