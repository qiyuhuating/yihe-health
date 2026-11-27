package handlers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/csrf"
	"yihe-care/internal/health"
)

func TestHealthAdmissionBoundedAndCancelable(t *testing.T) {
	a := newHealthWriteAdmission()
	for i := range healthWriteConcurrency {
		if !a.acquire(t.Context(), strconv.Itoa(i)) {
			t.Fatal("available admission slot rejected")
		}
	}
	for range healthWriteConcurrency {
		a.waiting <- struct{}{}
	}
	if a.acquire(t.Context(), "next") {
		t.Fatal("full queue admitted another request")
	}
	<-a.waiting
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if a.acquire(ctx, "next") {
		t.Fatal("canceled request acquired a slot")
	}
	// The wait budget must expire even when none of the active uploads finish.
	start := time.Now()
	if a.acquire(t.Context(), "next") || time.Since(start) > time.Second {
		t.Fatal("admission failed to bound queue wait")
	}
	a.release("0")
	if !a.acquire(t.Context(), "next") {
		t.Fatal("released capacity was not reusable")
	}
}

func TestSignedHealthAdmissionAndAtomicNonce(t *testing.T) {
	t.Setenv("YIHE_REQUIRE_SIGNED_DEVICE", "1")
	t.Setenv("HEALTH_LATE_DEVICE_LOCK", "0")
	pool := testDatabase(t)
	seedDevice(t, pool)
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	h, replica := testHandler(t, pool), testHandler(t, pool)
	const path = "/api/v1/device/healthBatch"
	body := healthLockOrderBody("signed-admission")
	nonce := "health-admission-0123456789"
	serve := func(handler *Handler, req *http.Request) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		handler.routeHealth(mux)
		app := csrf.Middleware(handler.Sessions, auth.Middleware(handler.Sessions,
			handler.EnforceAdminDuty(handler.EnforceDeviceSignatures(mux))))
		w := httptest.NewRecorder()
		app.ServeHTTP(w, req)
		return w
	}
	request := func(n string) *http.Request {
		return signedTestRequest(t, path, body, n, time.Now())
	}
	commitCount := func() int64 {
		t.Helper()
		var name string
		var n int64
		if err := pool.QueryRowContext(t.Context(), "SHOW SESSION STATUS LIKE 'Com_commit'").Scan(&name, &n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for i := range healthWriteConcurrency {
		if !h.healthWrites.acquire(t.Context(), strconv.Itoa(i)) {
			t.Fatal("fill active slots")
		}
		h.healthWrites.waiting <- struct{}{}
	}
	commits := commitCount()
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitBefore := pool.Stats().WaitCount
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	w := serve(h, request(nonce).WithContext(ctx))
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("overload response: HTTP %d %s", w.Code, w.Body)
	}
	if pool.Stats().WaitCount != waitBefore || commitCount() != commits {
		t.Fatal("rejected upload acquired a connection or committed a nonce")
	}
	for i := range healthWriteConcurrency {
		h.healthWrites.release(strconv.Itoa(i))
		<-h.healthWrites.waiting
	}
	if unsigned := serve(h, gateRequest(t, path, body)); unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("missing signature accepted: HTTP %d", unsigned.Code)
	}
	w = serve(h, request(nonce))
	if w.Code != http.StatusOK {
		t.Fatalf("admitted signed upload: HTTP %d %s", w.Code, w.Body)
	}
	if got := commitCount() - commits; got != 1 {
		t.Fatalf("signed health upload committed %d transactions, want one", got)
	}
	t.Log("signed health upload: 1 committed transaction (nonce and sample)")
	w = serve(replica, request(nonce))
	if w.Code != http.StatusConflict {
		t.Fatalf("cross-instance nonce replay: HTTP %d %s", w.Code, w.Body)
	}
	var n, samples int
	if err := pool.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM device_request_nonces").Scan(&n); err != nil || n != 1 {
		t.Fatalf("persisted nonces=%d err=%v", n, err)
	}
	if err := pool.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM health_sample").Scan(&samples); err != nil || samples != 1 {
		t.Fatalf("persisted samples=%d err=%v", samples, err)
	}
}

func TestHealthAdmissionHotDeviceCannotOccupyOtherSlots(t *testing.T) {
	a := newHealthWriteAdmission()
	if !a.acquire(t.Context(), "hot") {
		t.Fatal("first hot device rejected")
	}
	result := make(chan bool, 1)
	go func() { result <- a.acquire(t.Context(), "hot") }()
	deadline := time.After(time.Second)
	for len(a.waiting) == 0 {
		select {
		case <-deadline:
			t.Fatal("second hot upload did not queue")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	for i := range healthWriteConcurrency - 1 {
		if !a.acquire(t.Context(), strconv.Itoa(i)) {
			t.Fatal("hot device consumed unrelated slot")
		}
	}
	select {
	case <-result:
		t.Fatal("same-device upload obtained concurrent slot")
	default:
	}
	a.release("hot")
	select {
	case ok := <-result:
		if !ok {
			t.Fatal("hot device was not admitted on release")
		}
	case <-deadline:
		t.Fatal("hot device did not wake")
	}
	a.release("hot")
	for i := range healthWriteConcurrency - 1 {
		a.release(strconv.Itoa(i))
	}
	if len(a.active) != 0 || len(a.waiting) != 0 {
		t.Fatal("admission did not release slots")
	}
}

func TestHealthAdmissionHotDeviceLeavesConnectionForAnotherOwner(t *testing.T) {
	t.Setenv("HEALTH_LATE_DEVICE_LOCK", "0")
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(2,'second resident')")
	mustExec(t, pool, "INSERT INTO device(device_id,device_no,secret,old_id) SELECT 2,'WATCH002',secret,2 FROM device WHERE device_id=1")
	pool.SetMaxOpenConns(3)
	pool.SetMaxIdleConns(3)
	h := testHandler(t, pool)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	release := holdHealthOwnerClock(t, ctx, pool)
	defer release()
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- callAPI(h.healthBatch, healthLockOrderBody("hot-first")) }()
	if err := waitHealthClockContention(ctx, pool); err != nil {
		t.Fatal(err)
	}
	before := pool.Stats().WaitCount
	queued := make(chan *httptest.ResponseRecorder, healthWriteConcurrency-1)
	for i := range healthWriteConcurrency - 1 {
		go func() { queued <- callAPI(h.healthBatch, healthLockOrderBody("hot-wait-"+strconv.Itoa(i))) }()
	}
	cold := healthLockOrderBody("cold-owner")
	cold["deviceNo"] = "WATCH002"
	cold["samples"].([]health.Sample)[0].SourceDevice = "WATCH002"
	result := make(chan *httptest.ResponseRecorder, 1)
	started := time.Now()
	go func() { result <- callAPI(h.healthBatch, cold) }()
	select {
	case w := <-result:
		if w.Code != http.StatusOK {
			t.Fatalf("other owner: %d %s", w.Code, w.Body)
		}
		t.Logf("unrelated health batch finished while hot owner was locked: %v", time.Since(started))
	case <-ctx.Done():
		t.Fatal("other owner blocked by hot-device backlog")
	}
	for range healthWriteConcurrency - 1 {
		select {
		case w := <-queued:
			if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "1" {
				t.Fatalf("hot retry: %d %s", w.Code, w.Body)
			}
		case <-ctx.Done():
			t.Fatal("hot device did not obey queue deadline")
		}
	}
	if got := pool.Stats().WaitCount - before; got != 0 {
		t.Fatalf("local duplicate uploads caused %d database pool waits", got)
	}
	release()
	select {
	case w := <-first:
		if w.Code != http.StatusOK {
			t.Fatalf("first upload: %d %s", w.Code, w.Body)
		}
	case <-ctx.Done():
		t.Fatal("first upload did not finish")
	}
	var n int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_sample").Scan(&n); err != nil || n != 2 {
		t.Fatalf("persisted samples=%d err=%v", n, err)
	}
	t.Log("seven hot-device retries, zero additional pool waits, two durable samples")
}

func TestSOSPriorityBlocksNewUploadsWithoutHoldingConnection(t *testing.T) {
	t.Setenv("YIHE_SOS_PRIORITY", "1")
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	pool.SetMaxOpenConns(2)
	if !h.healthWrites.acquire(t.Context(), "WATCH001") {
		t.Fatal("initial health admission")
	}
	// Admission alone is not a device lock: SOS must still be able to
	// proceed on its reserved pool when a health writer has not started SQL.
	prelock := callAPI(h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "requestId": "priority-prelock"})
	if prelock.Code != http.StatusOK {
		t.Fatalf("pre-lock SOS %d %s", prelock.Code, prelock.Body)
	}
	tx, err := pool.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var deviceID int64
	if err = tx.QueryRowContext(t.Context(), "SELECT device_id FROM device WHERE device_id=1 FOR UPDATE").Scan(&deviceID); err != nil {
		t.Fatal(err)
	}
	if !h.healthWrites.beginTransaction("WATCH001", true) {
		t.Fatal("transaction boundary was not available")
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- callAPI(h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "requestId": "priority-sos"})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.healthWrites.mu.Lock()
		pending := h.healthWrites.urgent["WATCH001"] != 0
		blocked := !h.healthWrites.tryAcquire("WATCH001")
		h.healthWrites.mu.Unlock()
		if pending {
			if !blocked || pool.Stats().InUse != 1 {
				t.Fatal("SOS waiter holds SQL or permits another health upload")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("verified SOS never entered priority gate")
		}
		time.Sleep(time.Millisecond)
	}
	for i := range healthWriteUrgentConcurrency - 1 {
		if !h.healthWrites.acquire(t.Context(), strconv.Itoa(i)) {
			t.Fatal("unrelated upload should retain capacity")
		}
	}
	h.healthWrites.mu.Lock()
	extra := h.healthWrites.tryAcquire("extra")
	h.healthWrites.mu.Unlock()
	if extra {
		t.Fatal("emergency capacity not reserved")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	h.healthWrites.finishTransaction("WATCH001")
	select {
	case response := <-done:
		if response.Code != http.StatusOK {
			t.Fatalf("SOS response %d %s", response.Code, response.Body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SOS did not complete after health slot release")
	}
	h.healthWrites.release("WATCH001")
	for i := range healthWriteUrgentConcurrency - 1 {
		h.healthWrites.release(strconv.Itoa(i))
	}
	if !h.healthWrites.acquire(t.Context(), "WATCH001") {
		t.Fatal("priority was not released after commit")
	}
	h.healthWrites.release("WATCH001")
	var incidents int
	if err := pool.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM care_incident").Scan(&incidents); err != nil || incidents != 2 {
		t.Fatalf("durable SOS count %d err=%v", incidents, err)
	}
}

func TestSOSPriorityCancellation(t *testing.T) {
	a := newHealthWriteAdmission()
	if !a.acquire(t.Context(), "same") {
		t.Fatal("health admission")
	}
	if !a.beginTransaction("same", true) {
		t.Fatal("transaction boundary was not available")
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := a.prioritizeSOS(ctx, "same", 1); done <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		a.mu.Lock()
		pending := a.urgentCount
		a.mu.Unlock()
		if pending == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SOS did not register")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatal(err)
	}
	a.mu.Lock()
	pending := a.urgentCount
	a.mu.Unlock()
	if pending != 0 {
		t.Fatal("cancelled SOS leaked priority")
	}
	a.release("same")
}

func TestSOSPriorityWinsBeforeTransactionBoundary(t *testing.T) {
	a := newHealthWriteAdmission()
	if !a.acquire(t.Context(), "same") {
		t.Fatal("health admission")
	}
	release, err := a.prioritizeSOS(t.Context(), "same", 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.beginTransaction("same", true) {
		t.Fatal("ordinary upload started after SOS claimed priority")
	}
	if !a.acquire(t.Context(), "other") || !a.beginTransaction("other", true) {
		t.Fatal("SOS blocked an unrelated device")
	}
	a.release("other")
	release()
	if !a.beginTransaction("same", true) {
		t.Fatal("ordinary upload could not proceed after SOS released priority")
	}
	a.finishTransaction("same")
	a.release("same")
}

func TestSOSPriorityDoesNotWaitForHealthPoolCheckout(t *testing.T) {
	t.Setenv("YIHE_SOS_PRIORITY", "1")
	t.Setenv("MYSQL_SOS_MAX_CONNS", "2")
	t.Setenv("HEALTH_LATE_DEVICE_LOCK", "0")
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	pool.SetMaxOpenConns(1)
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	before := pool.Stats().WaitCount
	upload := make(chan *httptest.ResponseRecorder, 1)
	go func() { upload <- callAPI(h.healthBatch, healthLockOrderBody("pool-waiter")) }()
	for pool.Stats().WaitCount == before {
		select {
		case <-ctx.Done():
			t.Fatal("health upload never reached the pool wait")
		case <-time.After(time.Millisecond):
		}
	}
	sos := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		sos <- callAPI(h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "requestId": "priority-pool-waiter"})
	}()
	select {
	case w := <-sos:
		if w.Code != http.StatusOK {
			t.Fatalf("SOS response %d %s", w.Code, w.Body)
		}
	case <-ctx.Done():
		t.Fatal("SOS waited for an ordinary writer without a connection")
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case w := <-upload:
		if w.Code != http.StatusOK {
			t.Fatalf("health response %d %s", w.Code, w.Body)
		}
	case <-ctx.Done():
		t.Fatal("health upload did not complete after pool release")
	}
	var n int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_sample").Scan(&n); err != nil || n != 1 {
		t.Fatalf("durable health samples=%d err=%v", n, err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_incident").Scan(&n); err != nil || n != 1 {
		t.Fatalf("durable SOS incidents=%d err=%v", n, err)
	}
}

type beginFaultConnector struct {
	driver.Connector
	beginErrors []error
	begins      int
	closed      int
	beforeRetry func()
}

func (c *beginFaultConnector) Connect(context.Context) (driver.Conn, error) {
	if c.begins > 0 && c.beforeRetry != nil {
		c.beforeRetry()
	}
	return &beginFaultConn{owner: c}, nil
}

type beginFaultConn struct {
	driver.Conn
	owner *beginFaultConnector
}

func (c *beginFaultConn) Close() error { c.owner.closed++; return nil }

func (c *beginFaultConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	i := c.owner.begins
	c.owner.begins++
	if i < len(c.owner.beginErrors) && c.owner.beginErrors[i] != nil {
		return nil, c.owner.beginErrors[i]
	}
	return beginFaultTx{}, nil
}

type beginFaultTx struct{}

func (beginFaultTx) Commit() error   { return nil }
func (beginFaultTx) Rollback() error { return nil }

func TestSOSPriorityBeginRetriesOnlyBadConnection(t *testing.T) {
	other := errors.New("uncertain begin failure")
	for _, tc := range []struct {
		name   string
		errs   []error
		want   error
		begins int
	}{
		{"recovers", []error{driver.ErrBadConn}, nil, 2},
		{"bounded", []error{driver.ErrBadConn, driver.ErrBadConn, driver.ErrBadConn}, driver.ErrBadConn, 3},
		{"other error", []error{other}, other, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newHealthWriteAdmission()
			if !a.acquire(t.Context(), "watch") {
				t.Fatal("admission")
			}
			defer a.release("watch")
			c := &beginFaultConnector{beginErrors: tc.errs}
			pool := sql.OpenDB(c)
			defer pool.Close()
			tx, conn, err := a.beginPriorityTx(t.Context(), pool, "watch", false)
			if !errors.Is(err, tc.want) || c.begins != tc.begins {
				t.Fatalf("begin count=%d err=%v", c.begins, err)
			}
			if err == nil {
				if !a.active["watch"] || c.closed != 1 {
					t.Fatal("claim or discarded connection missing")
				}
				tx.Rollback()
				conn.Close()
				a.finishTransaction("watch")
			} else if a.active["watch"] || pool.Stats().InUse != 0 {
				t.Fatal("failed begin leaked claim or connection")
			}
		})
	}
}

func TestSOSPriorityCanClaimBetweenBeginRetries(t *testing.T) {
	a := newHealthWriteAdmission()
	if !a.acquire(t.Context(), "watch") {
		t.Fatal("admission")
	}
	defer a.release("watch")
	c := &beginFaultConnector{beginErrors: []error{driver.ErrBadConn}}
	var release func()
	c.beforeRetry = func() {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		var err error
		release, err = a.prioritizeSOS(ctx, "watch", 1)
		if err != nil {
			t.Fatal(err)
		}
	}
	pool := sql.OpenDB(c)
	defer pool.Close()
	tx, conn, err := a.beginPriorityTx(t.Context(), pool, "watch", false)
	if release != nil {
		defer release()
	}
	if !errors.Is(err, errHealthWriteBusy) || tx != nil || conn != nil || c.begins != 1 || pool.Stats().InUse != 0 {
		t.Fatalf("SOS lost retry boundary: begins=%d err=%v", c.begins, err)
	}
}

func TestHealthAdmissionHandsOffToOldestEligibleWaiter(t *testing.T) {
	a := newHealthWriteAdmission()
	if !a.acquire(t.Context(), "hot", 1) {
		t.Fatal("initial admission")
	}
	first, second := make(chan bool, 1), make(chan bool, 1)
	waitFor := func(count int) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			a.mu.Lock()
			n := len(a.waiters)
			a.mu.Unlock()
			if n == count {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("waiter did not enter queue")
			}
			time.Sleep(time.Millisecond)
		}
	}
	go func() { first <- a.acquire(t.Context(), "hot", 1) }()
	waitFor(1)
	go func() { second <- a.acquire(t.Context(), "hot", 1) }()
	waitFor(2)
	// An unrelated domain must not be blocked by the hot device.
	if !a.acquire(t.Context(), "cold", 2) {
		t.Fatal("unrelated device blocked")
	}
	a.release("cold")
	a.release("hot")
	// The first waiter owns the slot already, even before its goroutine runs.
	a.mu.Lock()
	_, reserved := a.active["hot"]
	pending := len(a.waiters)
	a.mu.Unlock()
	if !reserved || pending != 1 {
		t.Fatalf("slot not handed off: reserved=%t pending=%d", reserved, pending)
	}
	if !<-first {
		t.Fatal("first waiter rejected")
	}
	select {
	case <-second:
		t.Fatal("second waiter bypassed first")
	default:
	}
	a.release("hot")
	if !<-second {
		t.Fatal("second waiter rejected")
	}
	a.release("hot")
	if len(a.waiting) != 0 || len(a.active) != 0 || len(a.owners) != 0 {
		t.Fatal("admission leaked capacity")
	}
}

func TestHealthAdmissionCanceledWaiterDoesNotConsumeHandoff(t *testing.T) {
	a := newHealthWriteAdmission()
	if !a.acquire(t.Context(), "hot", 1) {
		t.Fatal("initial admission")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- a.acquire(ctx, "hot", 1) }()
	deadline := time.Now().Add(time.Second)
	for len(a.waiting) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("waiter did not enter queue")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	a.release("hot")
	if <-done {
		t.Fatal("canceled waiter acquired")
	}
	if !a.acquire(t.Context(), "hot", 1) {
		t.Fatal("cancellation leaked domain")
	}
	a.release("hot")
}
