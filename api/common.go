package api

import (
	"errors"
	"fmt"

	"golang.org/x/net/context"

	"github.com/dropbox/changes-artifacts/common/sentry"
	"github.com/martini-contrib/render"
)

// Post a JSON-serialized error message and statuscode on the HTTP response object (using Martini render).
// Log the error message to Sentry.
func LogAndRespondWithErrorf(ctx context.Context, render render.Render, code int, errStr string, params ...interface{}) {
	msg := fmt.Sprintf(errStr, params)
	sentry.ReportError(ctx, errors.New(msg))
	render.JSON(code, map[string]string{"error": msg})
}
