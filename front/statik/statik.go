package statik

import (
	_ "embed"

	"github.com/rakyll/statik/fs"
)

//go:embed assets.zip
var assetsZIP string

func init() {
	fs.Register(assetsZIP)
}
