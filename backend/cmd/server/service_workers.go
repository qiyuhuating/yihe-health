package main

import (
	"context"
	"sync"

	"yihe-care/internal/care"
	"yihe-care/internal/config"
	dbstore "yihe-care/internal/db"
	"yihe-care/internal/handlers"
	"yihe-care/internal/notify"
	"yihe-care/internal/rules"
	"yihe-care/internal/securestore"
	"yihe-care/internal/securityaudit"
	"yihe-care/internal/telemetryworkflow"
)

type databaseWorkerDeps struct {
	pool            *dbstore.Database
	box             *securestore.Box
	runCapabilities func(context.Context)
	heartbeats      *telemetryworkflow.HeartbeatBuffer
}

func startDatabaseWorkers(ctx context.Context, deps databaseWorkerDeps) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		var jobs sync.WaitGroup
		jobs.Add(8)
		go func() { defer jobs.Done(); rules.Run(ctx, deps.pool, deps.box) }()
		go func() { defer jobs.Done(); notify.Run(ctx, deps.pool, deps.box) }()
		go func() {
			defer jobs.Done()
			securityaudit.Run(ctx, deps.pool, config.Secret("SECURITY_ALERT_WEBHOOK_URL"), config.Secret("SECURITY_ALERT_WEBHOOK_TOKEN"))
		}()
		go func() {
			defer jobs.Done()
			if deps.runCapabilities != nil {
				deps.runCapabilities(ctx)
			}
		}()
		go func() { defer jobs.Done(); care.Run(ctx, deps.pool, deps.box) }()
		go func() { defer jobs.Done(); handlers.RunSignedDeviceNonceCleanup(ctx, deps.pool) }()
		go func() { defer jobs.Done(); handlers.RunDurableTelemetry(ctx, deps.pool, deps.box) }()
		go func() {
			defer jobs.Done()
			if deps.heartbeats != nil {
				deps.heartbeats.Run(ctx)
			}
		}()
		jobs.Wait()
	}()
	return done
}
