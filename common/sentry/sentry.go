package sentry

import (
	"fmt"
	"log"
	"net/http"

	"golang.org/x/net/context"

	"github.com/dropbox/changes-artifacts/common"
	"github.com/dropbox/changes-artifacts/common/reqcontext"
	"github.com/getsentry/raven-go"
	"github.com/go-martini/martini"
)

// getSentryClient returns a reporter which logs to Sentry if sentryDsn is provided.
// Logs to standard logger if sentryDsn is nil.
func getSentryClient(env string, sentryDsn string) *raven.Client {
	if sentryDsn == "" {
		return nil
	}

	sentryClient, err := raven.NewClient(sentryDsn, map[string]string{
		"version": common.GetVersion(),
		"env":     env,
	})

	if err != nil {
		log.Println("Error creating a Sentry client:", err)
		return nil
	}

	return sentryClient
}

type key int

const errReporterKey key = 0

// CreateAndInstallSentryClient installs a Sentry client to the supplied context.
// If an empty dsn is provided, the installed client will be nil.
func CreateAndInstallSentryClient(ctx context.Context, env string, dsn string) context.Context {
	sentryClient := getSentryClient(env, dsn)
	if sentryClient != nil {
		ctx = context.WithValue(ctx, errReporterKey, sentryClient)
	}

	return ctx
}

func getErrorReporterFromContext(ctx context.Context) *raven.Client {
	r, ok := ctx.Value(errReporterKey).(*raven.Client)
	if !ok {
		// If no error reporter is installed, we'll use log.Print methods.
		return nil
	}

	return r
}

// ReportError logs an error to Sentry if a sentry client is installed in the context.
// Logs to standard logger otherwise.
func ReportError(ctx context.Context, err error) {
	sentryClient := getErrorReporterFromContext(ctx)
	reportError(ctx, sentryClient, err)
}

// ReportMessage logs a message to Sentry if a sentry client is installed.
// Logs to standard logger otherwise.
func ReportMessage(ctx context.Context, msg string) {
	sentryClient := getErrorReporterFromContext(ctx)

	reportMessage(ctx, sentryClient, msg)
}

func reportError(ctx context.Context, sentryClient *raven.Client, err error) {
	if sentryClient != nil {
		req, ok := reqcontext.ReqFromContext(ctx)

		if ok {
			sentryClient.CaptureError(err, map[string]string{}, raven.NewHttp(req))
		} else {
			sentryClient.CaptureError(err, map[string]string{})
		}
	} else {
		log.Printf("[Sentry Error] %s\n", err)
	}
}

func reportMessage(ctx context.Context, sentryClient *raven.Client, msg string) {
	if sentryClient != nil {
		req, ok := reqcontext.ReqFromContext(ctx)

		if ok {
			sentryClient.CaptureMessage(msg, map[string]string{}, raven.NewHttp(req))
		} else {
			sentryClient.CaptureMessage(msg, map[string]string{})
		}
	} else {
		log.Printf("[Sentry Message] %s\n", msg)
	}
}

// PanicHandler intercepts panic's from request handlers and sends them to Sentry.
// Exception is re-panic'd to be handled up the chain.
func PanicHandler() martini.Handler {
	return func(res http.ResponseWriter, req *http.Request, ctx context.Context, c martini.Context) {
		defer func() {
			if e := recover(); e != nil {
				log.Printf("Caught exception %v\n", e)
				if err, ok := e.(error); ok {
					ReportError(ctx, err)
				} else {
					ReportMessage(ctx, fmt.Sprintf("Caught error %s", e))
				}
				panic(e)
			}
		}()
		c.Next()
	}
}
