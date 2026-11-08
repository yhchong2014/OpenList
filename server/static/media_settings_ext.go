package static

import "strings"

// injectManageMediaSettingsExt 向页面 HTML（conf.ManageHtml 与 conf.IndexHtml）注入轻量扩展脚本：
// 无论是直接访问 /@manage/media/settings 还是从首页 SPA 路由进入管理后台，当使用未包含
// media_date_added_follow_release 白名单的前端构建（如外部 CDN 资源或旧浏览器缓存）时，自动复用页面已有的
// Hope UI Switch 样式渲染「入库时间跟随发行日期」开关；若构建产物已原生渲染 #media_date_added_follow_release，
// 则脚本自动跳过，不重复挂载。
func injectManageMediaSettingsExt(manageHtml string) string {
	if strings.Contains(manageHtml, `id="openlist-media-date-added-ext"`) {
		return manageHtml
	}
	if strings.Contains(manageHtml, "</body>") {
		return strings.Replace(manageHtml, "</body>", mediaDateAddedExtScript+"</body>", 1)
	}
	return manageHtml + mediaDateAddedExtScript
}

const mediaDateAddedExtScript = `<script id="openlist-media-date-added-ext">
(function () {
  var KEY = "media_date_added_follow_release";
  var currentChecked = null;

  function apiRoot() {
    var cfg = window.OPENLIST_CONFIG || {};
    var base = cfg.base_path || "";
    if (base === "/") base = "";
    if (base && base.charAt(0) !== "/") base = "/" + base;
    if (base.endsWith("/")) base = base.slice(0, -1);
    var api = cfg.api || "/";
    if (api === "/") api = location.origin + base;
    if (api.endsWith("/")) api = api.slice(0, -1);
    return api;
  }

  function authToken() {
    try {
      return localStorage.getItem("token") || "";
    } catch (_) {
      return "";
    }
  }

  function isMediaSettingsRoute() {
    return /\/@manage\/media\/settings\/?$/.test(location.pathname);
  }

  function uiLangMode(anchorCard) {
    var txt = anchorCard ? (anchorCard.textContent || "") : "";
    if (/[\u4e00-\u9fff]/.test(txt) && !/[\u3040-\u30ff]/.test(txt)) {
      if (/透過|圖片|預設|網域|伺服器/.test(txt)) return "zh-TW";
      return "zh-CN";
    }
    var lang = "";
    try {
      lang = localStorage.getItem("lang") || navigator.language || "";
    } catch (_) {}
    if (/^zh[-_](tw|hk|mo|hant)/i.test(lang)) return "zh-TW";
    if (/^zh/i.test(lang) && txt === "") return "zh-CN";
    return "en";
  }

  function syncSwitchDOM(card, checked) {
    currentChecked = !!checked;
    var input = card.querySelector("input#" + KEY);
    if (input) input.checked = !!checked;
    var nodes = card.querySelectorAll("label.hope-switch, [class*='hope-switch'], [class*='switch__control'], [class*='switch__thumb']");
    for (var i = 0; i < nodes.length; i++) {
      if (checked) {
        nodes[i].setAttribute("data-checked", "");
      } else {
        nodes[i].removeAttribute("data-checked");
      }
    }
  }

  // 拦截后台保存设置 XHR（/api/admin/setting/save），确保点击页面 Save 按钮不会用旧缓存覆盖当前开关状态
  var origOpen = XMLHttpRequest.prototype.open;
  var origSend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.open = function (method, url) {
    this.__olUrl = typeof url === "string" ? url : "";
    return origOpen.apply(this, arguments);
  };
  XMLHttpRequest.prototype.send = function (body) {
    if (currentChecked !== null && typeof body === "string" && this.__olUrl && this.__olUrl.indexOf("/admin/setting/save") !== -1) {
      try {
        var arr = JSON.parse(body);
        if (Array.isArray(arr)) {
          var found = false;
          for (var i = 0; i < arr.length; i++) {
            if (arr[i] && arr[i].key === KEY) {
              arr[i].value = currentChecked ? "true" : "false";
              found = true;
            }
          }
          if (found) body = JSON.stringify(arr);
        }
      } catch (_) {}
    }
    return origSend.call(this, body);
  };

  function mountIfNeeded() {
    if (!isMediaSettingsRoute()) return;
    if (document.getElementById(KEY)) return;
    var anchorInput = document.getElementById("media_proxy_cover");
    if (!anchorInput) return;
    var anchorCard = anchorInput.closest(".hope-form-control") || anchorInput.parentElement && anchorInput.parentElement.parentElement;
    if (!anchorCard || !anchorCard.parentElement) return;

    var mode = uiLangMode(anchorCard);
    var zh = mode === "zh-CN" || mode === "zh-TW";
    var card = anchorCard.cloneNode(true);
    card.setAttribute("data-ol-ext", KEY);

    var titleLabel = card.querySelector("label[for='media_proxy_cover']");
    if (titleLabel) {
      titleLabel.setAttribute("for", KEY);
      titleLabel.textContent = mode === "zh-TW"
        ? "入庫時間跟隨發行日期"
        : (mode === "zh-CN" ? "入库时间跟随发行日期" : "Sync Date Added to Release Date");
    }
    var statusSpan = document.createElement("span");
    statusSpan.style.cssText = "margin-left:8px;font-size:12px;opacity:0.75;font-weight:normal;";
    if (titleLabel) titleLabel.appendChild(statusSpan);

    var input = card.querySelector("input#media_proxy_cover") || card.querySelector("input[type='checkbox']");
    if (!input) return;
    input.id = KEY;

    var helper = card.querySelector(".hope-form-helper-text") || card.lastElementChild;
    if (helper && helper !== input && helper.tagName !== "LABEL") {
      helper.textContent = mode === "zh-TW"
        ? "開啟後所有媒體的入庫時間（Date Added）將同步為上映/發行日期（Release Date），對已有條目立即生效並在後續掃描、刮削、編輯、匯入時持續保持。"
        : (mode === "zh-CN"
            ? "开启后所有媒体的入库时间（Date Added）将同步为上映/发行日期（Release Date），对已有条目立即生效并在后续扫描、刮削、编辑、导入时持续保持。"
            : "When enabled, Date Added (created_at) for all media items automatically follows Release Date — applied immediately to existing items and kept in sync on future scans, scrapes, edits, and imports.");
    }

    syncSwitchDOM(card, false);
    anchorCard.parentElement.appendChild(card);

    // 拉取当前服务端开关值
    fetch(apiRoot() + "/api/admin/setting/get?key=" + KEY, {
      headers: { Authorization: authToken() }
    })
      .then(function (r) { return r.json(); })
      .then(function (resp) {
        if (resp && resp.code === 200 && resp.data) {
          syncSwitchDOM(card, resp.data.value === "true" || resp.data.value === "1");
        }
      })
      .catch(function () {});

    input.addEventListener("change", function () {
      var next = !!input.checked;
      syncSwitchDOM(card, next);
      statusSpan.textContent = mode === "zh-TW" ? "儲存中..." : (zh ? "保存中..." : "Saving...");
      fetch(apiRoot() + "/api/admin/media/date_added_follow_release", {
        method: "POST",
        headers: {
          "Content-Type": "application/json;charset=utf-8",
          Authorization: authToken()
        },
        body: JSON.stringify({ enabled: next })
      })
        .then(function (r) { return r.json(); })
        .then(function (resp) {
          if (resp && resp.code === 200) {
            var n = resp.data && typeof resp.data.synced === "number" ? resp.data.synced : 0;
            statusSpan.textContent = next
              ? (mode === "zh-TW" ? ("✓ 已同步 " + n + " 筆") : (zh ? ("✓ 已同步 " + n + " 条") : ("✓ Synced " + n + " items")))
              : (mode === "zh-TW" ? "✓ 已關閉" : (zh ? "✓ 已关闭" : "✓ Disabled"));
          } else {
            statusSpan.textContent = (resp && resp.message) ? resp.message : "Error";
          }
        })
        .catch(function () {
          statusSpan.textContent = mode === "zh-TW" ? "儲存失敗" : (zh ? "保存失败" : "Save failed");
        });
    });
  }

  var timer = null;
  function scheduleCheck() {
    if (timer) return;
    timer = setTimeout(function () {
      timer = null;
      mountIfNeeded();
    }, 80);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", scheduleCheck);
  } else {
    scheduleCheck();
  }
  window.addEventListener("popstate", scheduleCheck);
  window.addEventListener("hashchange", scheduleCheck);
  var obs = new MutationObserver(scheduleCheck);
  obs.observe(document.documentElement, { childList: true, subtree: true });
})();
</script>`
