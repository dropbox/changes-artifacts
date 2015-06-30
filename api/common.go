package api

import (
	"fmt"
	"github.com/martini-contrib/render"
)

// Printf-like function for returning a json-serialized error reponse
func JsonErrorf(render render.Render, code int, errStr string, params ...interface{}) {
	render.JSON(code, map[string]string{"error": fmt.Sprintf(errStr, params)})
}
