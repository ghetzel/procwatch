package procwatch

import (
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger(`procwatch`)
var Version = `0.1.2`

func SetLogBackend(backend logging.LeveledBackend) {
	log.SetBackend(backend)
}
