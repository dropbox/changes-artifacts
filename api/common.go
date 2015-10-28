package api

import (
	"errors"
	"fmt"
	"math/rand"

	"golang.org/x/net/context"

	"github.com/dropbox/changes-artifacts/common/sentry"
	"github.com/martini-contrib/render"
)

// LogAndRespondWithErrorf posts a JSON-serialized error message and statuscode on the HTTP response
// object (using Martini render).
// Log the error message to Sentry.
func LogAndRespondWithErrorf(ctx context.Context, render render.Render, code int, errStr string, params ...interface{}) {
	msg := fmt.Sprintf(errStr, params)
	sentry.ReportError(ctx, errors.New(msg))
	render.JSON(code, map[string]string{"error": msg})
}

// LogAndRespondWithError posts a JSON-serialized error and statuscode on the HTTP response object
// (using Martini render).
// Log the error message to Sentry.
func LogAndRespondWithError(ctx context.Context, render render.Render, code int, err error) {
	sentry.ReportError(ctx, err)
	render.JSON(code, map[string]string{"error": err.Error()})
}

// RespondWithErrorf posts a JSON-serialized error message and statuscode on the HTTP response
// object (using Martini render).
func RespondWithErrorf(ctx context.Context, render render.Render, code int, errStr string, params ...interface{}) {
	msg := fmt.Sprintf(errStr, params)
	render.JSON(code, map[string]string{"error": msg})
}

// RespondWithError posts a JSON-serialized error and statuscode on the HTTP response object
// (using Martini render).
func RespondWithError(ctx context.Context, render render.Render, code int, err error) {
	render.JSON(code, map[string]string{"error": err.Error()})
}

const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randString(n int) string {
	strBytes := make([]byte, n)
	for i := range strBytes {
		strBytes[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(strBytes)
}
