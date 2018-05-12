package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
)

var (
	flagDebug = flag.Bool("rtest-debug", false, "Turn on inotify debug information")
)

func main() {
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to get working dir", err)
	}

	watcher, err := initWatches(wd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	go handleEvents(watcher)
	go handleEnter(wd)

	sigs := make(chan os.Signal)
	signal.Notify(sigs, os.Interrupt, os.Kill)

	// when we get a signal, close the watcher and exit
	<-sigs

	fmt.Fprintln(os.Stderr, "Exiting")
	if err = watcher.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func initWatches(workingDir string) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create watcher")
	}

	err = filepath.Walk(workingDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return errors.Wrapf(err, "error occurred while walking: %s", path)
		}

		if !info.IsDir() {
			return nil
		}

		if filepath.Base(path) == "vendor" {
			return nil
		}

		debugln("Adding watch:", path)
		if err := watcher.Add(path); err != nil {
			return errors.Wrap(err, "failed to add watch to %s")
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return watcher, nil
}

func handleEvents(watcher *fsnotify.Watcher) error {
	throttle := make(map[string]time.Time)

	for {
		select {
		case err := <-watcher.Errors:
			if err == nil {
				return nil
			}
			debugln("watching error:", err)
			return err
		case ev := <-watcher.Events:
			debugln("watcher event:", ev.Name, ev.Op.String())

			now := time.Now()
			key := ev.Name + ":" + ev.Op.String()

			t, ok := throttle[key]
			if ok {
				elapsed := now.Sub(t) / time.Millisecond
				if elapsed < 800 {
					debugln("skipping event, less than 800ms")
					continue
				}
			}

			throttle[key] = now

			if err := handleEvent(watcher, ev); err != nil {
				return err
			}
		}
	}
}

func handleEvent(watcher *fsnotify.Watcher, ev fsnotify.Event) error {
	switch {
	case ev.Op&fsnotify.Create == fsnotify.Create:
		// We don't care if it's a folder or not since if it's a file we're not going to
		// watch it anyway, and if it's a file called vendor we're doubly not going to watch it.
		// So we can do this before we know what kind of thing it is.
		if base := filepath.Base(ev.Name); base == "vendor" {
			return nil
		}

		fi, err := os.Stat(ev.Name)
		if err != nil {
			return errors.Wrapf(err, "failed to stat newly created file")
		}

		if !fi.IsDir() {
			return runTestsForFile(ev.Name)
		}

		debugln("Adding watch:", ev.Name)
		if err := watcher.Add(ev.Name); err != nil {
			return errors.Wrapf(err, "error removing watch on %s", ev.Name)
		}
	case ev.Op&fsnotify.Write == fsnotify.Write:
		if err := runTestsForFile(ev.Name); err != nil {
			return err
		}
		// This code actually doesn't seem necessary. I guess when something is deleted the watch
		// is probably autoremoved. Removing the watch manually like this caused problems in the past.
		//
		//case ev.Op&fsnotify.Remove == fsnotify.Remove || ev.Op&fsnotify.Rename == fsnotify.Rename:
		/*debugln("Removing watch:", ev.Name)
		if err := watcher.Remove(ev.Name); err != nil {
			return errors.Wrapf(err, "error removing watch on %s", ev.Name)
		}*/
	}

	return nil
}

// handleEnter doesn't necessarily need to be done like this
// we could get into stty calls and all that to hide echoing the output
// etc. but we just do the naive thing.
func handleEnter(wd string) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if err := runTestsForDir(wd); err != nil {
			fmt.Fprintln(os.Stderr, "error running go test", err)
		}
	}
}

func runTestsForDir(dir string) error {
	return runGoTest(dir)
}

func runTestsForFile(file string) error {
	filename := filepath.Base(file)
	dir := filepath.Dir(file)
	ext := filepath.Ext(filename)

	if ext != ".go" {
		return nil
	}

	return runGoTest(dir)
}

func runGoTest(dir string) error {
	args := []string{"test"}
	otherArgs := flag.Args()
	args = append(args, otherArgs...)

	debugln("running: go", strings.Join(args, " "))

	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func debugln(args ...interface{}) {
	if *flagDebug {
		fmt.Fprintln(os.Stderr, args...)
	}
}

func debugf(format string, args ...interface{}) {
	if *flagDebug {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}
