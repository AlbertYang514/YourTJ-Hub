package routes

import (
	"io/fs"
	"testing/fstest"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/service/decorationassetservice"
)

type decorationStaticFS struct{ embedded fs.FS }

func (f decorationStaticFS) Open(name string) (fs.File, error) {
	if data, modified, err := decorationassetservice.ReadAsset(name); err == nil {
		return fstest.MapFS{name: &fstest.MapFile{Data: data, Mode: 0444, ModTime: modified}}.Open(name)
	}
	return f.embedded.Open(name)
}
