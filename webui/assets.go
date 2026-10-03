/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package webui

import (
	"embed"
	"encoding/base64"
	"strings"
)

//go:embed assets/index.html assets/app.css assets/i18n.js assets/app.js assets/logo.png
var assets embed.FS

func asset(name string) string {
	b, err := assets.ReadFile("assets/" + name)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// pageHTML inlines the stylesheet and scripts into one document, which is
// loaded with NavigateToString; no files are served or fetched.
func pageHTML() string {
	html := asset("index.html")
	html = strings.Replace(html, `<link rel="stylesheet" href="app.css">`, "<style>"+asset("app.css")+"</style>", 1)
	logo := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte(asset("logo.png")))
	html = strings.Replace(html, `<script src="i18n.js"></script>`, `<script>window.__LOGO="`+logo+`";</script><script>`+asset("i18n.js")+"</script>", 1)
	html = strings.Replace(html, `<script src="app.js"></script>`, "<script>"+asset("app.js")+"</script>", 1)
	return html
}

// bootstrapScript runs before the page and installs the bridge transport.
func bootstrapScript() string {
	return `window.__host = {
  post: function (msg) { window.chrome.webview.postMessage(msg); }
};`
}
