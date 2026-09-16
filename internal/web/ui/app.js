"use strict";

/*
  画面側。素の JavaScript だけで書く。枠組みも束ね役も使わない。

  この頁が守ること。

  - 判断を1つも持たない。状態バッジも件数も、待ち受けが渡したものをそのまま描く。
    ここで数え始めると、internal/diff が避けている誤検出を作り直すことになる。
  - 文言を1つも持たない。すべて /api/bootstrap の目録から来る。
  - 行の中身を innerHTML に渡さない。訳には <i> のような字が実際に入っている
    （ゲームの書式）。textContent で入れれば、その字はその字として見える。
  - 取りにいく先は自分自身だけ。外向きの通信はこの頁からも出さない。
  - 題名に行の中身を入れない。題名はブラウザーの履歴に残る。
*/

(function () {
  var ui = null;
  var locales = [];

  var el = {
    title: document.getElementById("app-title"),
    localeLabel: document.getElementById("locale-label"),
    locale: document.getElementById("locale"),
    reload: document.getElementById("reload"),
    rows: document.getElementById("rows"),
    readonly: document.getElementById("readonly-notice"),
    message: document.getElementById("message"),
    path: document.getElementById("file-path"),
    notes: document.getElementById("notes"),
    countsTitle: document.getElementById("counts-title"),
    counts: document.getElementById("counts"),
    statsTitle: document.getElementById("stats-title"),
    stats: document.getElementById("stats"),
    list: document.getElementById("list")
  };

  /*
    文言を引く。置換は {name} の1形式だけ。複数形の規則は持ち込まない。
    Go 側の expand と同じ規則にしてある。
  */
  function t(key, params) {
    var text = ui && ui.messages ? ui.messages[key] : null;
    if (!text) {
      return key;
    }
    if (!params) {
      return text;
    }
    return text.replace(/\{(\w+)\}/g, function (whole, name) {
      return Object.prototype.hasOwnProperty.call(params, name)
        ? String(params[name])
        : whole;
    });
  }

  function clear(node) {
    node.replaceChildren();
  }

  function span(className, text) {
    var e = document.createElement("span");
    if (className) {
      e.className = className;
    }
    e.textContent = text === undefined || text === null ? "" : String(text);
    return e;
  }

  function li(className, text) {
    var e = document.createElement("li");
    if (className) {
      e.className = className;
    }
    e.textContent = text;
    return e;
  }

  function showMessage(text) {
    if (!text) {
      el.message.hidden = true;
      el.message.textContent = "";
      return;
    }
    el.message.textContent = text;
    el.message.hidden = false;
  }

  /* 取りにいく先は同じ生成元だけ。相対のパスしか書かない。 */
  function getJSON(path) {
    return fetch(path, {
      credentials: "same-origin",
      headers: { Accept: "application/json" }
    }).then(function (res) {
      if (!res.ok) {
        throw new Error(String(res.status));
      }
      return res.json();
    });
  }

  function applyCatalog() {
    document.documentElement.lang = ui.lang;
    document.documentElement.dir = ui.dir;
    document.title = t("app.title");
    el.title.textContent = t("app.title");
    el.localeLabel.textContent = t("ui.locale");
    el.reload.textContent = t("ui.reload");
    el.countsTitle.textContent = t("ui.counts");
    el.statsTitle.textContent = t("ui.stats");
    el.readonly.textContent = t("ui.readonly_notice");
  }

  function fillLocales(selected) {
    clear(el.locale);
    if (!selected) {
      var placeholder = document.createElement("option");
      placeholder.value = "";
      placeholder.textContent = t("ui.select_locale");
      el.locale.appendChild(placeholder);
    }
    locales.forEach(function (name) {
      var option = document.createElement("option");
      /* ロケール名はデータ側の語彙なので、そのまま出す。訳さない。 */
      option.value = name;
      option.textContent = name;
      if (name === selected) {
        option.selected = true;
      }
      el.locale.appendChild(option);
    });
  }

  function renderNotes(notes) {
    clear(el.notes);
    (notes || []).forEach(function (note) {
      el.notes.appendChild(li(null, note));
    });
  }

  /*
    件数。判定できていないカテゴリは数を出さず、待ち受けが付けた理由を出す。
    0 件と書くと「もう何も残っていない」と読まれる。
  */
  function renderCounts(counts) {
    clear(el.counts);
    (counts || []).forEach(function (c) {
      var item = document.createElement("li");
      item.className = c.status + (c.judged ? "" : " held");
      item.appendChild(span(null, c.statusLabel + " / " + c.label + " "));
      item.appendChild(
        span(
          null,
          c.judged
            ? t("ui.count_value", { count: c.count })
            : t("ui.not_judged", { reason: c.reason })
        )
      );
      el.counts.appendChild(item);
    });
  }

  function renderStats(stats) {
    clear(el.stats);
    (stats || []).forEach(function (s) {
      el.stats.appendChild(li(null, s.label + ": " + s.value));
    });
  }

  function badgeNode(badge) {
    var e = span("badge " + badge.status, badge.label);
    if (badge.note) {
      /* 理由は待ち受けが付けたもの。行の中身ではない。 */
      e.title = badge.note;
    }
    return e;
  }

  /*
    1行を組む。

    訳の欄には入力欄を置かない。1839行ぶんの入力欄を常設すると、開くだけで
    重くなり、次の段で編集を足すときに作りを変えることになる。編集は
    「選んだ1行にだけ入力欄を差し込む」形で足せるよう、いまは文字だけ置く。
  */
  function rowNode(line, locale) {
    var row = document.createElement("div");
    row.className = "row" + (line.editable ? "" : " not-editable");

    row.appendChild(span("cell num", line.n));

    var badges = document.createElement("div");
    badges.className = "cell badges";
    (line.badges || []).forEach(function (b) {
      badges.appendChild(badgeNode(b));
    });
    row.appendChild(badges);

    row.appendChild(span("cell speaker", line.speaker));

    /* 原文は常に英語。右から左の訳に引きずられて崩れないよう ltr に固定する。 */
    var source = span("cell source", line.source);
    source.dir = "ltr";
    row.appendChild(source);

    var translation = span("cell translation", line.translation);
    /*
      訳の向きは中身から決めさせる（ヘブライ語の確認用）。lang はロケール名を
      そのまま入れる。字形の選び方がこれで変わる。
    */
    translation.dir = "auto";
    translation.lang = locale;
    if (!line.editable && line.reason) {
      translation.className = "cell reason";
      translation.textContent = t("ui.not_editable", { reason: line.reason });
    }
    row.appendChild(translation);

    return row;
  }

  function headingNode(line) {
    var e = document.createElement("div");
    e.className = "heading " + (line.heading || "other");
    /* ファイルにあるコメント行をそのまま出す。組み直さない。 */
    e.textContent = line.text;
    return e;
  }

  function renderLines(data) {
    var fragment = document.createDocumentFragment();
    (data.lines || []).forEach(function (line) {
      if (line.kind === "heading") {
        fragment.appendChild(headingNode(line));
        return;
      }
      fragment.appendChild(rowNode(line, data.locale));
    });
    el.list.replaceChildren(fragment);
    /* 行数は待ち受けが数えたもの。ここでは数えない。 */
    el.rows.textContent = t("ui.rows", { count: data.rows });
  }

  function render(data) {
    el.path.textContent = t("ui.file") + ": " + data.path;
    renderNotes(collectNotes(data));
    renderCounts(data.counts);
    renderStats(data.stats);
    renderLines(data);
  }

  /*
    断り書きを1つにまとめる。文面はすべて待ち受けから来たもので、
    ここで新しい判断はしない（source 列があるかどうかは、待ち受けが読んだ
    ファイルのヘッダーそのもの）。
  */
  function collectNotes(data) {
    var notes = (data.notes || []).slice();
    if (!data.sourceColumn) {
      notes.push(t("ui.no_source"));
    }
    if (data.readOnlyReason) {
      notes.push(t("ui.file_readonly", { reason: data.readOnlyReason }));
    }
    return notes;
  }

  function load(locale) {
    if (!locale) {
      clear(el.list);
      el.rows.textContent = "";
      showMessage(t("ui.select_locale"));
      return;
    }
    showMessage(t("ui.loading"));
    /* URL に載せるのはロケール名だけ。原文も訳も URL には載せない。 */
    getJSON("/api/lines?locale=" + encodeURIComponent(locale))
      .then(function (data) {
        showMessage("");
        render(data);
      })
      .catch(function () {
        /* 失敗の中身は出さない。翻訳者にできるのは読み直すことだけ。 */
        showMessage(t("ui.load_failed"));
      });
  }

  function boot() {
    getJSON("/api/bootstrap")
      .then(function (data) {
        ui = data.ui;
        locales = data.locales || [];
        applyCatalog();
        fillLocales(data.selected);
        el.locale.addEventListener("change", function () {
          load(el.locale.value);
        });
        el.reload.addEventListener("click", function () {
          load(el.locale.value);
        });
        load(data.selected || "");
      })
      .catch(function () {
        showMessage("ui.load_failed");
      });
  }

  boot();
})();
