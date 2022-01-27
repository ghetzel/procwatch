package procwatch

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ghetzel/diecast"
	"github.com/ghetzel/go-stockutil/log"
	"github.com/husobee/vestigo"
	"github.com/urfave/negroni"
)

//go:embed ui
//go:embed ui/_layouts
//go:embed ui/*/_*
var embedded embed.FS

var DefaultAddress = `:9001`

type Server struct {
	Address     string `json:"address"                ini:"address"`
	UiDirectory string `json:"ui_directory,omitempty" ini:"ui_directory"`
	manager     *Manager
}

func (self *Server) Initialize(manager *Manager) error {
	self.manager = manager

	if self.Address == `` {
		self.Address = DefaultAddress
	}

	if self.UiDirectory == `` {
		self.UiDirectory = `embedded`
	}

	return nil
}

func (self *Server) Start() error {
	var uiDir = self.UiDirectory
	var serverHandler = negroni.New()
	var router = vestigo.NewRouter()

	if self.UiDirectory == `` {
		self.UiDirectory = `embedded`
	}
	if d := os.Getenv(`UI`); d != `` {
		self.UiDirectory = d
	}
	if self.UiDirectory == `embedded` {
		uiDir = `/`
	}

	var ui = diecast.NewServer(uiDir, `*.html`)

	if !log.Debugging() {
		ui.Log.Destination = os.DevNull
	}

	if self.UiDirectory == `embedded` {
		if sub, err := fs.Sub(embedded, `ui`); err == nil {
			ui.SetFileSystem(http.FS(sub))
		} else {
			return fmt.Errorf("fs: %v", err)
		}
	}

	if err := ui.Initialize(); err != nil {
		log.Error(err)
		return err
	}

	// routes not registered below will fallback to the UI server
	vestigo.CustomNotFoundHandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ui.ServeHTTP(w, req)
	})

	router.Get(`/api/status`, func(w http.ResponseWriter, req *http.Request) {
		Respond(w, map[string]interface{}{
			`version`: Version,
		})
	})

	router.Get(`/api/manager`, func(w http.ResponseWriter, req *http.Request) {
		Respond(w, self.manager)
	})

	router.Get(`/api/programs`, func(w http.ResponseWriter, req *http.Request) {
		Respond(w, self.manager.Programs())
	})

	router.Get(`/api/programs/:program`, func(w http.ResponseWriter, req *http.Request) {
		var name = vestigo.Param(req, `program`)

		if program, ok := self.manager.Program(name); ok {
			Respond(w, program)
		} else {
			http.Error(w, fmt.Sprintf("Program '%s' not found", name), http.StatusNotFound)
		}
	})

	router.Put(`/api/programs/:program/action/:action`, func(w http.ResponseWriter, req *http.Request) {
		var name = vestigo.Param(req, `program`)
		var action = strings.ToLower(vestigo.Param(req, `action`))

		if program, ok := self.manager.Program(name); ok {
			switch action {
			case `start`:
				program.Start()

			case `stop`:
				program.Stop()

			case `restart`:
				program.Stop()
				program.Start()

			default:
				http.Error(w, fmt.Sprintf("Unknown action '%s'", action), http.StatusBadRequest)
			}

			http.Error(w, ``, http.StatusNoContent)
		} else {
			http.Error(w, fmt.Sprintf("Program '%s' not found", name), http.StatusNotFound)
		}
	})

	serverHandler.UseHandler(router)

	log.Infof("Running API server at %s", self.Address)

	var server = &http.Server{
		Addr:           self.Address,
		Handler:        serverHandler,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Error(err)
		return err
	}

	return nil
}

func Respond(w http.ResponseWriter, data interface{}) {
	w.Header().Set(`Content-Type`, `application/json`)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
