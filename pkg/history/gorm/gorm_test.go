package gorm

// TODO testdata

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	testhist "github.com/pancsta/asyncmachine-go/pkg/history/test"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	amss "github.com/pancsta/asyncmachine-go/pkg/states"
)

type testLogger struct {
	t *testing.T
}

func (l *testLogger) Printf(format string, v ...interface{}) {
	l.t.Logf(format, v...)
}

var ss = amss.BasicStates

func TestGormSchema(t *testing.T) {
	os.Remove(t.Name() + ".sqlite")

	ctx := context.Background()
	// init memory
	onErr := func(err error) {
		t.Error(err)
	}
	db, _, err := NewDb("", false)
	require.NoError(t, err)
	cfg := Config{
		BaseConfig: amhist.BaseConfig{
			TrackedStates: ss.Names(),
		},
	}

	// init machine and history
	mach := am.New(ctx, amss.BasicSchema, &am.Opts{Id: "MyMach2"})
	_, err = NewMemory(ctx, db, mach, cfg, onErr)
	require.NoError(t, err)
}

func TestGormTrack(t *testing.T) {
	os.Remove(t.Name() + ".sqlite")
	// debug := true
	debug := false

	// configs
	rounds := 50
	onErr := func(err error) {
		t.Error(err)
	}
	cfg := Config{
		BaseConfig: amhist.BaseConfig{
			TrackedStates: am.S{ss.Start},
		},
		QueueBatch: 10,
	}

	// init basic machine
	ctx := context.Background()
	mach := am.New(ctx, amss.BasicSchema, &am.Opts{Id: "MyMach2"})

	// init memory
	db, _, err := NewDb(t.Name(), debug)
	require.NoError(t, err)
	mem, err := NewMemory(ctx, db, mach, cfg, onErr)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, mem.Dispose())
	}()

	// common test
	testhist.AssertBasics(t, mem, rounds)
}

func TestGormTrackMany(t *testing.T) {
	os.Remove(t.Name() + ".sqlite")
	// debug := true
	debug := false

	// configs
	rounds := 50000
	onErr := func(err error) {
		t.Error(err)
	}
	cfg := Config{
		BaseConfig: amhist.BaseConfig{
			TrackedStates: am.S{ss.Start},
			MaxRecords:    10e6,
		},
		QueueBatch: 1000,
	}

	// init basic machine
	ctx := context.Background()
	mach := am.New(ctx, amss.BasicSchema, &am.Opts{Id: "MyMach2"})

	// init memory
	db, _, err := NewDb(t.Name(), debug)
	require.NoError(t, err)
	mem, err := NewMemory(ctx, db, mach, cfg, onErr)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, mem.Dispose())
	}()

	// common test
	testhist.AssertBasics(t, mem, rounds)
}

func TestGormPostgresTrackMany(t *testing.T) {
	t.Skip("skipping docker-based tests")

	// debug := true
	debug := false

	// configs
	rounds := 50000
	onErr := func(err error) {
		t.Error(err)
	}
	cfg := Config{
		BaseConfig: amhist.BaseConfig{
			TrackedStates: am.S{ss.Start},
			MaxRecords:    10e6,
		},
		QueueBatch: int32(min(rounds, 100)),
	}

	// init basic machine
	ctx := context.Background()
	mach := am.New(ctx, amss.BasicSchema, &am.Opts{Id: "MyMach2"})

	// set up postgres container
	pgContainer, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("test-db"),
		postgres.WithUsername("user"),
		postgres.WithPassword("password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(5*time.Second)),
	)
	require.NoError(t, err)
	defer func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Fatalf("failed to terminate container: %s", err)
		}
	}()

	// 3. Get the dynamic connection string from the container.
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// init memory
	cfgLog := logger.Config{
		SlowThreshold:             time.Second,
		LogLevel:                  logger.Silent,
		Colorful:                  true,
		IgnoreRecordNotFoundError: true,
	}
	if debug {
		cfgLog.LogLevel = logger.Info
		cfgLog.IgnoreRecordNotFoundError = false
	}
	db, err := gorm.Open(gormpg.Open(connStr), &gorm.Config{
		Logger: logger.New(&testLogger{t: t}, cfgLog),
	})
	require.NoError(t, err)
	mem, err := NewMemory(ctx, db, mach, cfg, onErr)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, mem.Dispose())
	}()

	// common test
	testhist.AssertBasics(t, mem, rounds)
}

func ExampleNewMemory() {

	// configs
	onErr := func(err error) {
		panic(err)
	}
	cfg := Config{
		QueueBatch: 10,
		BaseConfig: amhist.BaseConfig{
			TrackedStates: am.S{ss.Start},
		},
	}

	// init basic machine
	ctx := context.Background()
	mach := am.New(ctx, amss.BasicSchema, &am.Opts{Id: "MyMach2"})

	// init memory
	db, _, _ := NewDb("mydb", false)
	mem, _ := NewMemory(ctx, db, mach, cfg, onErr)

	// query etc

	_ = mem.Dispose()
}
