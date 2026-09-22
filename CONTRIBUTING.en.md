# Contribution guide

**[日本語](CONTRIBUTING.md)** | English

Changes to this repository are accepted through issues and pull requests.

## How to proceed

1. Open an issue before you make the change and write what you want to change. Small fixes are fine without an issue
1. Create a working branch from `develop` (`feature/name-of-the-change`)
1. Make the change and confirm that `npm run lint` passes
1. Open a pull request against `develop`. Follow the template and write what you changed and why

## How branches are used

We follow GitFlow.

| Branch | Role |
| ---- | ---- |
| `main` | What has been released. Tags are placed here |
| `develop` | The main line of development for the next release |
| `feature/*` | Adding or fixing a feature. Branched from `develop` and merged back into `develop` |
| `release/*` | Preparing a release. The "Release" workflow branches it from `develop` and merges it into `main` |
| `hotfix/*` | An urgent fix to what has been released. Branched from `main`, and the version in `package.json` is bumped on that branch too. Merging it into `main` publishes it, and it is merged back into `develop` as well |

The steps for a release and for an urgent fix are in "Branches and releases" in the [README](README.en.md).
Pull requests are merged with a merge commit (Create a merge commit).

Do not make `main` or `develop` the head of a pull request.
The repository is set to delete the head branch automatically after a merge, so you could lose that branch itself.
To go from `develop` to `main`, the release workflow creates a `release/*` branch.
To go from `main` to `develop`, use a `merge/*` branch.
The reasons and how to recover are in [CLAUDE.md](CLAUDE.md) (Japanese only).

## How to write documents

The Japanese documents are checked with `textlint` and `markdownlint`.
The rules live in the published shared configuration [@223n/lint-config-ja](https://www.npmjs.com/package/@223n/lint-config-ja).

- Keep the style consistent in "desu/masu" form
- Write one sentence per line
- Do not put a space between full-width and half-width characters. Half-width words are easier to read inside a code span

Warnings that can be fixed locally are fixed by `npm run lint:md:fix` and `npm run lint:ja:fix`.
After fixing them, look at the diff and make sure nothing changed that you did not intend.

## How to write scripts

`scripts/setup.sh` and `scripts/setup.ps1` do the same thing.
Do not change only one of them.
The way arguments are written (`--dry-run` and `-DryRun`) differs.
Keep the messages they print and the exit codes aligned.
For rewriting names, `scripts/setup.sh` uses Node and `scripts/setup.ps1` uses PowerShell string replacement.
That is why they need different tools.

- `scripts/setup.ps1` assumes PowerShell 7 or later. It does not run on Windows PowerShell 5.1
- Save `.ps1` as UTF-8 without a BOM, with LF line endings. PowerShell 7 reads it as UTF-8 even without a BOM
- Decide whether a native command succeeded with `$LASTEXITCODE`. `if (gh ...)` looks at the output, so it is always false for a call made with `--silent`
- `npm run lint` only checks the Japanese documents. Scripts are not covered

## Commit messages

Write in Japanese what you changed and why.
Keep the first line to about 50 characters, and write the detailed reasons in the body after a blank line.

## Labels

Labels for issues and pull requests are managed in `.github/labels.yml`.
When you add or change a label, change this file rather than the GitHub screen.
Once it lands on `develop`, the sync workflow runs and the labels on the repository are brought in line with the file.
A sync from the `main` side does not delete labels that are missing from the file.
