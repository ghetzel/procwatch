package procwatch

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/diecast"
	"github.com/husobee/vestigo"
	"github.com/urfave/negroni"
	"net/http"
	"strings"
)

var DefaultAddress = `:9001`

type Server struct {
	Address     string `json:"address"`
	UiDirectory string `json:"ui_directory,omitempty"`
	manager     *Manager
}

func (self *Server) Initialize(manager *Manager) error {
	self.manager = manager

	if self.Address == `` {
		self.Address = DefaultAddress
	}

	if self.UiDirectory == `` {
		self.UiDirectory = `./ui` // TODO: this will be "embedded" after development settles
	}

	return nil
}

func (self *Server) Start() error {
	uiDir := self.UiDirectory

	if self.UiDirectory == `embedded` {
		uiDir = `/`
	}

	server := negroni.New()
	router := vestigo.NewRouter()
	ui := diecast.NewServer(uiDir, `*.html`)

	if err := ui.Initialize(); err != nil {
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
		name := vestigo.Param(req, `program`)

		if program, ok := self.manager.Program(name); ok {
			Respond(w, program)
		} else {
			http.Error(w, fmt.Sprintf("Program '%s' not found", name), http.StatusNotFound)
		}
	})

	router.Put(`/api/programs/:program/action/:action`, func(w http.ResponseWriter, req *http.Request) {
		name := vestigo.Param(req, `program`)
		action := strings.ToLower(vestigo.Param(req, `action`))

		log.Debugf("Action %s[%s]", name, action)

		if program, ok := self.manager.Program(name); ok {
			switch action {
			case `start`:
				switch program.GetState() {
				case ProgramStopped, ProgramFatal:
					program.Start()
				}

			case `stop`:
				switch program.GetState() {
				case ProgramStarting, ProgramRunning:
					program.Stop()
				}

			case `restart`:
				switch program.GetState() {
				case ProgramStarting, ProgramRunning:
					program.Stop()
				}

				switch program.GetState() {
				case ProgramStopped, ProgramFatal:
					program.Start()
				}

			default:
				http.Error(w, fmt.Sprintf("Unknown action '%s'", action), http.StatusBadRequest)
			}

			log.Debugf("Action %s[%s]: done", name, action)

			http.Error(w, ``, http.StatusNoContent)
		} else {
			http.Error(w, fmt.Sprintf("Program '%s' not found", name), http.StatusNotFound)
		}
	})

	server.UseHandler(router)

	log.Infof("Running API server at %s", self.Address)
	server.Run(self.Address)

	return nil
}

func Respond(w http.ResponseWriter, data interface{}) {
	w.Header().Set(`Content-Type`, `application/json`)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
