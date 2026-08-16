// Package web содержит статику веб-интерфейса, вшитую в бинарник.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

// Static возвращает файловую систему с содержимым каталога static.
func Static() (fs.FS, error) {
	return fs.Sub(files, "static")
}
