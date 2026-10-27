package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gemmadesktop.local/app/internal/app"
	"gemmadesktop.local/app/internal/desktop"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var frontend embed.FS

func main() {
	serve := flag.String("serve", "", "Development HTTP listener, loopback only (e.g. 127.0.0.1:8787)")
	data := flag.String("data-dir", "", "Application data directory")
	flag.Parse()
	core, e := app.Open(*data)
	if e != nil {
		log.Fatal(e)
	}
	defer core.Close()
	assets, e := fs.Sub(frontend, "frontend/dist")
	if e != nil {
		log.Fatal(e)
	}
	if *serve != "" {
		if e = serveDev(core, assets, *serve); e != nil {
			log.Fatal(e)
		}
		return
	}
	service := desktop.New(core)
	native := application.New(application.Options{Name: "Gemma Desktop", Description: "A local workspace for thoughtful agents", Services: []application.Service{application.NewService(service)}, Assets: application.AssetOptions{Handler: secureAssets(application.AssetFileServerFS(assets))}, Mac: application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false}})
	core.PickFolder = func() (string, error) {
		return native.Dialog.OpenFile().CanChooseFiles(false).CanChooseDirectories(true).SetTitle("Choose a workspace").PromptForSingleSelection()
	}
	core.OpenPath = func(path string, reveal bool) error {
		args := []string{path}
		if reveal {
			args = []string{"-R", path}
		}
		return exec.Command("/usr/bin/open", args...).Run()
	}
	menu := native.NewMenu()
	menu.AddRole(application.AppMenu)
	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)
	native.Menu.Set(menu)
	window := native.Window.NewWithOptions(application.WebviewWindowOptions{Name: "main", Title: "Gemma Desktop", Width: 1500, Height: 980, MinWidth: 960, MinHeight: 640, URL: "/", BackgroundColour: application.NewRGB(9, 13, 20), DevToolsEnabled: false})
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { window.Hide(); e.Cancel() })
	native.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(e *application.ApplicationEvent) { window.Show() })
	if e = native.Run(); e != nil {
		log.Print(e)
	}
}

// The browser development transport rejects cross-origin requests. It is not
// enabled by the packaged application and never binds to the LAN.
func serveDev(core *app.Core, assets fs.FS, addr string) error {
	host, _, e := net.SplitHostPort(addr)
	if e != nil {
		return e
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("development server must bind a loopback IP")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/call", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+addr {
			http.Error(w, "origin rejected", 403)
			return
		}
		if r.Host != addr {
			http.Error(w, "host rejected", 403)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "JSON required", 415)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
		var req struct {
			Method string  `json:"method"`
			Params app.Doc `json:"params"`
		}
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		v, e := core.Call(r.Context(), req.Method, req.Params)
		if e != nil {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]any{"error": e.Error()})
			return
		}
		json.NewEncoder(w).Encode(v)
	})
	mux.Handle("/", secureAssets(http.FileServerFS(assets)))
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(done)
	go func() {
		<-done
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()
	log.Printf("Gemma Desktop development UI: http://%s", addr)
	e = server.ListenAndServe()
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}

func secureAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
