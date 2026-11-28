package observability

import (
	"context"
	"time"
)

// TransactionTrace is an optional request-scoped diagnostic sink. It receives
// fixed stage names and elapsed durations, never credentials or SQL contents.
// Normal requests do not allocate a recorder or read the clock for tracing.
type TransactionTrace func(stage string, elapsed time.Duration)

type transactionTraceKey struct{}

func WithTransactionTrace(ctx context.Context, trace TransactionTrace) context.Context {
	return context.WithValue(ctx, transactionTraceKey{}, trace)
}

func TransactionTraceFrom(ctx context.Context) TransactionTrace {
	trace, _ := ctx.Value(transactionTraceKey{}).(TransactionTrace)
	return trace
}

func (t TransactionTrace) Start() time.Time {
	if t == nil {
		return time.Time{}
	}
	return time.Now()
}

func (t TransactionTrace) End(stage string, start time.Time) {
	if t != nil {
		t(stage, time.Since(start))
	}
}
