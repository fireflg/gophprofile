// Command server поднимает HTTP-API и веб-интерфейс сервиса аватарок.
package main

import (
	"os"

	"github.com/fireflg/gophprofile/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		logger.FatalStartup("server stopped with error", err)
		os.Exit(1)
	}
}
