package procwatch

import (
	"github.com/ghetzel/go-stockutil/log"
)

func LogOutput(program *Program, lines chan string, level string) {
	for line := range lines {
		log.Logf(log.GetLevel(level), "[%s] %s", program.Name, line)
	}
}
