package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"
)

// shim stands in for the Wails bindings when the page runs in a browser.
const shim = `<script>
window.go = {main: {App: new Proxy({}, {get: (_, name) => (...args) =>
  fetch("/api/" + name, {method: "POST", body: JSON.stringify(args)})
    .then(r => r.ok ? r.json() : r.text().then(t => Promise.reject(t)))})}};
</script>`

// runPreview serves the reader over plain HTTP for checking the page in a
// browser without the Wails window: kikoku2002 -preview :8091
func runPreview(addr string) {
	app := NewApp()
	app.startup(context.Background())
	dist, _ := fs.Sub(frontend, "frontend/dist")
	files := http.FileServer(http.FS(dist))
	assets := assetHandler{app}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/" || p == "/index.html":
			b, _ := fs.ReadFile(dist, "index.html")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(strings.Replace(string(b), "<head>", "<head>"+shim, 1)))
		case strings.HasPrefix(p, "/api/"):
			var args []json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&args)
			out, err := callAPI(app, strings.TrimPrefix(p, "/api/"), args)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			json.NewEncoder(w).Encode(out)
		case strings.HasPrefix(p, "/img/"), strings.HasPrefix(p, "/font/"), strings.HasPrefix(p, "/movie/"):
			assets.ServeHTTP(w, r)
		default:
			files.ServeHTTP(w, r)
		}
	})
	log.Println("preview on", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func callAPI(app *App, name string, args []json.RawMessage) (any, error) {
	arg := func(i int, v any) {
		if i < len(args) {
			_ = json.Unmarshal(args[i], v)
		}
	}
	switch name {
	case "Init":
		return app.Init(), nil
	case "Show":
		var i int
		var jumped bool
		arg(0, &i)
		arg(1, &jumped)
		return app.Show(i, jumped)
	case "SaveSettings":
		var s Settings
		arg(0, &s)
		return app.SaveSettings(s), nil
	case "Silence":
		app.Silence()
	}
	return nil, nil
}
