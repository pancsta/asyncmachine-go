package path_watcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/joho/godotenv"
	ss "github.com/pancsta/asyncmachine-go/examples/path_watcher/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func init() {
	// load .env
	_ = godotenv.Load()

	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	// amhelp.EnableDebugging(false)
	// amhelp.SetLogLevel(am.LogChanges)
}

// PathWatcher watches all dirs in PATH for changes and returns a list
// of executables.
type PathWatcher struct {
	am.ExceptionHandler

	Mach        *am.Machine
	ResultsLock sync.Mutex
	Results     []string
	EnvPath     string

	watcher     *fsnotify.Watcher
	dirCache    map[string][]string
	dirState    map[string]*am.Machine
	ongoing     map[string]context.Context
	lastRefresh map[string]time.Time
}

func New(ctx context.Context) (*PathWatcher, error) {
	w := &PathWatcher{
		EnvPath:     os.Getenv("PATH"),
		dirCache:    make(map[string][]string),
		dirState:    make(map[string]*am.Machine),
		ongoing:     make(map[string]context.Context),
		lastRefresh: make(map[string]time.Time),
	}
	opts := &am.Opts{
		Id: "watcher",
	}

	if os.Getenv("YASM_DEBUG") != "" {
		opts.HandlerTimeout = time.Minute
		opts.DontPanicToException = true
	}
	w.Mach = am.New(ctx, ss.States, opts)

	err := w.Mach.VerifyStates(ss.Names)
	if err != nil {
		return nil, err
	}

	err = w.Mach.BindHandlers(w)
	if err != nil {
		return nil, err
	}

	amhelp.MachDebugEnv(w.Mach)

	return w, nil
}

func (w *PathWatcher) InitState(e *am.Event) {
	var err error

	w.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		w.Mach.Remove1(ss.Init, nil)
		w.Mach.AddErr(err, nil)
	}
}

func (w *PathWatcher) InitEnd(e *am.Event) {
	w.watcher.Close()
}

func (w *PathWatcher) WatchingState(e *am.Event) {
	path := w.EnvPath
	dirs := strings.Split(path, string(os.PathListSeparator))

	// start the loop (bound to this instance)
	ctx := e.Machine().NewStateCtx(ss.Watching)
	go w.watchLoop(ctx)

	// subscribe
	for _, dirName := range dirs {

		// if path doesn't exist, continue
		if _, err := os.Stat(dirName); os.IsNotExist(err) {
			continue
		}

		// create state per dir
		err := w.watcher.Add(dirName)
		if err != nil {
			e.Machine().AddErr(err, nil)
		}

		// create a state for each dir
		state := am.New(ctx, ss.StatesDir, nil)
		err = state.VerifyStates(ss.NamesDir)
		if err != nil {
			e.Machine().AddErr(err, nil)
			continue
		}

		w.dirState[dirName] = state

		// schedule a refresh
		w.Mach.Add1(ss.Refreshing, am.A{"dirName": dirName})
	}
}

func (w *PathWatcher) WatchingEnd(e *am.Event) {
	paths := w.watcher.WatchList()

	for _, path := range paths {
		err := w.watcher.Remove(path)
		if err != nil {
			e.Machine().AddErr(err, nil)
		}
	}
}

func (w *PathWatcher) watchLoop(ctx context.Context) {
	for {
		select {

		case event, ok := <-w.watcher.Events:
			if !ok {
				w.Mach.Remove1(ss.Watching, nil)
				return
			}
			w.Mach.Add1(ss.ChangeEvent, am.A{
				"fsnotify.Event": event,
			})

		case err, ok := <-w.watcher.Errors:
			if !ok {
				w.Mach.Remove1(ss.Watching, nil)
				return
			}
			w.Mach.AddErr(err, nil)

		case <-ctx.Done():
			// state expired
			return
		}
	}
}

func (w *PathWatcher) ChangeEventState(e *am.Event) {
	defer e.Machine().Remove1(ss.ChangeEvent, nil)
	event := e.Args["fsnotify.Event"].(fsnotify.Event)

	// exe
	isRemove := event.Op&fsnotify.Remove == fsnotify.Remove
	if !isRemove {
		isExe, err := isExecutable(event.Name)
		if !isExe || err != nil {
			return
		}
	}
	dirName := filepath.Dir(event.Name)

	w.Mach.Add1(ss.Refreshing, am.A{
		"dirName": dirName,
	})
}

func (w *PathWatcher) ExceptionState(e *am.Event) {
	w.ExceptionHandler.ExceptionState(e)
}

func (w *PathWatcher) RefreshingEnter(e *am.Event) bool {
	// validate req params
	_, ok1 := e.Args["dirName"]
	dirName, ok2 := e.Args["dirName"].(string)
	dirState, ok3 := w.dirState[dirName]
	depsOk := ok1 && ok2 && ok3
	if !depsOk {
		return false
	}

	// let the debounced refreshes pass
	isDebounce, _ := e.Args["isDebounce"].(bool)
	if dirState.Is1(ss.Refreshing) || (dirState.Is1(ss.DirDebounced) && !isDebounce) {
		return false
	}

	return true
}

func (w *PathWatcher) RefreshingState(e *am.Event) {
	w.Mach.Remove1(ss.Refreshing, nil)

	dirName := e.Args["dirName"].(string)
	dirState := w.dirState[dirName]
	// TODO config
	debounce := time.Second

	// max 1 refresh per second
	since := time.Since(w.lastRefresh[dirName])
	shouldDebounce := since < debounce
	if dirState.Is1(ss.DirCached) && shouldDebounce {
		w.Mach.Log("Debounce for %s", dirName)
		dirState.Add1(ss.DirDebounced, nil)

		go func() {
			time.Sleep(debounce)
			w.Mach.Add1(ss.Refreshing, am.A{
				"dirName":    dirName,
				"isDebounce": true,
			})
		}()

		return
	}

	w.Mach.Log("Refreshing execs in %s", dirName)
	dirState.Add1(ss.Refreshing, nil)
	w.ongoing[dirName] = dirState.NewStateCtx(ss.Refreshing)
	ctx := w.ongoing[dirName]

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		executables, err := listExecutables(dirName)
		if err != nil {
			e.Machine().AddErr(err, nil)
		}

		w.Mach.Remove1(ss.Refreshing, am.A{
			"dirName": dirName,
		})
		w.Mach.Add1(ss.Refreshed, am.A{
			"dirName":     dirName,
			"executables": executables,
		})
	}()
}

func (w *PathWatcher) RefreshingExit(e *am.Event) bool {
	// GC
	_, ok := e.Args["dirName"]
	if ok {
		dirName, ok := e.Args["dirName"].(string)
		if ok {
			delete(w.ongoing, dirName)
		}
	}

	// check completions
	mut := e.Mutation()

	// removing Init is a force shutdown
	removeInit := mut.Type == am.MutationRemove && mut.IsCalled(w.Mach.Index1(ss.Init))

	return len(w.ongoing) == 0 || removeInit
}

func (w *PathWatcher) RefreshingEnd(e *am.Event) {
	// forced cleanup
	for i := range w.ongoing {
		delete(w.ongoing, i)
	}
}

func (w *PathWatcher) RefreshedEnter(e *am.Event) bool {
	// validate req params
	_, ok1 := e.Args["dirName"].(string)
	_, ok2 := e.Args["executables"].([]string)

	return ok1 && ok2
}

func (w *PathWatcher) RefreshedState(e *am.Event) {
	w.Mach.Remove1(ss.Refreshed, nil)

	dirName := e.Args["dirName"].(string)
	executables := e.Args["executables"].([]string)
	w.dirCache[dirName] = executables
	w.lastRefresh[dirName] = time.Now()

	// update the per-dir state
	w.dirState[dirName].Add(am.S{ss.Refreshed, ss.DirCached}, nil)

	// try to finish the whole refresh
	w.Mach.Add1(ss.AllRefreshed, nil)
}

func (w *PathWatcher) AllRefreshedEnter(e *am.Event) bool {
	return len(w.ongoing) == 0
}

func (w *PathWatcher) AllRefreshedState(e *am.Event) {
	w.ResultsLock.Lock()
	defer w.ResultsLock.Unlock()

	for _, executables := range w.dirCache {
		w.Results = append(w.Results, executables...)
	}
	w.Results = uniqueStrings(w.Results)
}

func (w *PathWatcher) Start() {
	w.Mach.Add1(ss.Init, nil)
}

func (w *PathWatcher) Stop() {
	w.Mach.Remove1(ss.Init, nil)
}

// /// HELPERS /////

func isExecutable(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	return info.Mode().Perm()&0o111 != 0, nil
}

func listExecutables(dirPath string) ([]string, error) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var executables []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fullPath := dirPath + "/" + file.Name()
		isExe, err := isExecutable(fullPath)
		if err != nil {
			continue
		}

		if isExe {
			executables = append(executables, file.Name())
		}
	}

	return executables, nil
}

func uniqueStrings(s []string) []string {
	seen := make(map[string]struct{})
	var result []string

	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}

	return result
}
