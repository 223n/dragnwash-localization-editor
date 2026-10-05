// 日本語の検査規則。公開されている共有設定 @223n/lint-config-ja の「ですます調」版を使う。
//
// 文体を「である調」にしたい場合は、次のように書き換える。
//   module.exports = require('@223n/lint-config-ja')
//
// 3.0.0で入った「普通体の文末」の規則（desumasu-ending）は、既存の文書に指摘が多いため、
// いまは desumasuEnding: false で外している。文書を直したら、この指定を消して規則を有効に戻す。
//
// 規則の理由は https://github.com/223n/node_japanese_lint_template を見よ。
const { createTextlintConfig } = require('@223n/lint-config-ja/config/textlint-base.js')

module.exports = createTextlintConfig({ style: 'ですます', desumasuEnding: false })
