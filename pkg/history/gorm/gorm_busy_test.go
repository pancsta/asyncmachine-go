package gorm

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	sqlitedriver "github.com/ncruces/go-sqlite3/driver"

	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// TestMemoryBusyRetry asserts that a busy database is retried instead of
// dropping the batch.
func TestMemoryBusyRetry(t *testing.T) {
	ctx := t.Context()
	name := filepath.Join(t.TempDir(), "busy")

	db, sqlDb, err := NewDb(name, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDb.Close() })

	// keep one conn, so the short busy timeout applies to all writes
	sqlDb.SetMaxOpenConns(1)
	sqlDb.SetMaxIdleConns(1)
	if err := db.Exec("PRAGMA busy_timeout=50").Error; err != nil {
		t.Fatal(err)
	}

	mach := am.New(ctx, am.Schema{"Foo": {}, "Bar": {}}, &am.Opts{
		Id: "busy-mach",
	})
	t.Cleanup(mach.Dispose)

	var onErrs atomic.Int32
	mem, err := NewMemory(ctx, db, mach, Config{}, func(err error) {
		onErrs.Add(1)
		t.Logf("history: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mem.Dispose() })

	// queue a transition, but don't flush it yet
	mach.Add1("Foo", nil)
	<-mach.When1("Foo", ctx)

	// hold the write lock from a second connection
	locker, err := sqlitedriver.Open(name + ".sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locker.Close() })
	conn, err := locker.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}

	syncErr := make(chan error, 1)
	go func() { syncErr <- mem.Sync(ctx) }()

	// wait until the flush hit a busy database
	deadline := time.Now().Add(10 * time.Second)
	for mem.MetricSqliteBusy.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no sqlite busy retry")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// release the lock, the retry should succeed
	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if err := <-syncErr; err != nil {
		t.Fatalf("sync: %v", err)
	}

	if n := onErrs.Load(); n != 0 {
		t.Fatalf("onErr calls: %d", n)
	}
	if n := mem.MetricDropped.Load(); n != 0 {
		t.Fatalf("dropped records: %d", n)
	}

	records, err := mem.FindLatest(ctx, false, 10, amhist.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
}

// TestMemoryInitBusy asserts that a busy database during init returns an error
// instead of leaving a half-initialized memory.
func TestMemoryInitBusy(t *testing.T) {
	ctx := t.Context()
	name := filepath.Join(t.TempDir(), "init")

	db, sqlDb, err := NewDb(name, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDb.Close() })

	sqlDb.SetMaxOpenConns(1)
	sqlDb.SetMaxIdleConns(1)
	if err := db.Exec("PRAGMA busy_timeout=50").Error; err != nil {
		t.Fatal(err)
	}
	// migrate upfront, so only the machine init can fail
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	mach := am.New(ctx, am.Schema{"Foo": {}}, &am.Opts{Id: "init-mach"})
	t.Cleanup(mach.Dispose)

	locker, err := sqlitedriver.Open(name + ".sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locker.Close() })
	conn, err := locker.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}

	mem, err := NewMemory(ctx, db, mach,
		Config{SkipMigrate: true}, func(err error) {
			t.Logf("history: %v", err)
		})
	if err == nil {
		_ = mem.Dispose()
		t.Fatal("expected a busy init error")
	}

	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
}
