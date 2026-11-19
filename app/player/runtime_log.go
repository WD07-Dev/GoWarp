package player

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"runtime/pprof"
	"slices"
	"sync"
	"time"
)

type runtimeLogKey struct{}

type runtimeLog struct {
	file     *os.File
	messages chan string
	stop     chan int
	done     chan struct{}
	once     sync.Once
}

func startRuntimeDiagnostics(args []string, appID string) (*runtimeLog, error) {
	if !slices.Contains(args, "--debug") {
		return nil, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return startRuntimeLog(filepath.Join(cache, "gowarp", "logs"), appID)
}

func startRuntimeLog(dir, appID string) (*runtimeLog, error) {
	name := appID + ".log"
	if appID == "" || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return nil, fmt.Errorf("invalid log app ID %q", appID)
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	if err := debug.SetCrashOutput(file, debug.CrashOptions{}); err != nil {
		file.Close()
		return nil, err
	}
	d := &runtimeLog{file: file, messages: make(chan string, 32), stop: make(chan int, 1), done: make(chan struct{})}
	writer := log.New(file, "", log.Ldate|log.Ltime|log.Lmicroseconds|log.LUTC)
	writer.Printf("start os=%s arch=%s go=%s cpus=%d gomaxprocs=%d pid=%d", runtime.GOOS, runtime.GOARCH, runtime.Version(), runtime.NumCPU(), runtime.GOMAXPROCS(0), os.Getpid())
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
		{Name: "/gc/cycles/total:gc-cycles"},
		{Name: "/gc/heap/allocs:bytes"},
		{Name: "/cpu/classes/gc/total:cpu-seconds"},
		{Name: "/cpu/classes/gc/mark/assist:cpu-seconds"},
	}
	memory := func() {
		metrics.Read(samples)
		// Cumulative allocation and GC counters reveal churn between samples.
		writer.Printf("memory heap_objects_bytes=%d go_committed_bytes=%d gc_cycles=%d goroutines=%d heap_allocated_bytes=%d gc_cpu_seconds=%.6f gc_assist_cpu_seconds=%.6f",
			samples[0].Value.Uint64(), samples[1].Value.Uint64()-samples[2].Value.Uint64(), samples[3].Value.Uint64(), runtime.NumGoroutine(),
			samples[4].Value.Uint64(), samples[5].Value.Float64(), samples[6].Value.Float64())
	}
	memory()
	go func() {
		defer close(d.done)
		defer file.Close()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		// Keep profiling and file I/O off the game thread.
		profilePath := filepath.Join(dir, appID+".cpu.pprof")
		profile, profileErr := os.OpenFile(profilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if profileErr == nil {
			profileErr = pprof.StartCPUProfile(profile)
			if profileErr != nil {
				profile.Close()
				os.Remove(profilePath)
				profile = nil
			}
		}
		if profileErr != nil {
			writer.Printf("cpu_profile error=%q", profileErr)
		} else {
			writer.Printf("cpu_profile start path=%q", profilePath)
		}
		stopProfile := func() {
			if profile == nil {
				return
			}
			pprof.StopCPUProfile()
			if err := profile.Close(); err != nil {
				writer.Printf("cpu_profile error=%q", err)
			} else {
				writer.Printf("cpu_profile saved path=%q", profile.Name())
			}
			profile = nil
		}
		defer stopProfile()
		/*
			Bound routine diagnostics per run. The runtime's crash descriptor
			can still append its complete stack trace after this budget is used.
		*/
		remaining := 4 << 20
		write := func(message string) {
			if remaining <= 0 {
				return
			}
			writer.Print(message)
			memory()
			remaining -= len(message) + 512
		}
		for {
			select {
			case message := <-d.messages:
				write(message)
			case <-ticker.C:
				if remaining > 0 {
					memory()
					remaining -= 512
				}
			case code := <-d.stop:
				for {
					select {
					case message := <-d.messages:
						write(message)
					default:
						stopProfile()
						allocationPath := filepath.Join(dir, appID+".allocs.pprof")
						allocationFile, err := os.OpenFile(allocationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
						if err == nil {
							err = pprof.Lookup("allocs").WriteTo(allocationFile, 0)
							closeErr := allocationFile.Close()
							if err == nil {
								err = closeErr
							}
						}
						if err != nil {
							writer.Printf("allocation_profile error=%q", err)
						} else {
							writer.Printf("allocation_profile saved path=%q", allocationPath)
						}
						writer.Printf("exit code=%d", code)
						file.Sync()
						return
					}
				}
			}
		}
	}()
	return d, nil
}

func (d *runtimeLog) record(message string) {
	if d == nil {
		return
	}
	if len(message) > 4096 {
		message = message[:4096] + "..."
	}
	select {
	case <-d.done:
	case d.messages <- message:
	default:
	}
}

func (d *runtimeLog) finish(code int) {
	if d == nil {
		return
	}
	d.once.Do(func() {
		d.stop <- code
		<-d.done
		if err := debug.SetCrashOutput(nil, debug.CrashOptions{}); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	})
}
