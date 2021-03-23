package main

import (
	"flag"
	"io"
	"io/ioutil"
	"log"
	"math"
	"time"
	"net/http"
	"os"
//	"os/exec"
	"path/filepath"
        "strings"
	"github.com/fsnotify/fsnotify"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)


var  debugOn bool = true

func debug(f string, x ...interface{}) {
	if debugOn {
		log.Printf(f, x...)
	}
}



//////////////////////
type Event = fsnotify.Event
type Op = fsnotify.Op

const (
	Create Op = fsnotify.Create
	Write     = fsnotify.Write
	Remove    = fsnotify.Remove
	Rename    = fsnotify.Rename
	Chmod     = fsnotify.Chmod
)

func fatal(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// Watcher is like fsnotify.Watcher but also notifies on changes to symlink targets


// Event returns the next event.
func (w *FileWatcher) Event() (e Event, err error) {
	return w.EventTimeout(time.Duration(math.MaxInt64))
}

// EventTimeout returns the next event or os.ErrDeadlineExceeded if timeout is exceeded.
func (w *FileWatcher) EventTimeout(timeout time.Duration) (e Event, err error) {
	var ok bool
	select {
	case e, ok = <-w.watcher.Events:
	case err, ok = <-w.watcher.Errors:
	case <-time.After(timeout):
		return Event{}, os.ErrDeadlineExceeded
	}
	switch {
	case !ok:
		return Event{}, io.EOF
	case e.Op == Create:
		debug("e.Op is Create for e.Name %v", e.Name)
		if info, err := os.Lstat(e.Name); err == nil {
			if isSymlink(info) {
				debug("watcher.Add %v",e.Name)
				_ = w.watcher.Add(e.Name)
			}
		}
	case e.Op == Remove:
		debug("e.Op is Remove for e.Name %v", e.Name)
		w.watcher.Remove(e.Name)
	case e.Op == Chmod || e.Op == Rename:
		debug("e.Op - Rename or Chmod for %v %v",e.Op, e.Name)
		if info, err := os.Lstat(e.Name); err == nil {
			if isSymlink(info) {
				// Symlink target may have changed.
			//	rawfilename := os.Readlink(e.Name)
				_ = w.watcher.Remove(e.Name)
				//os.Create(rawfilename)
				_ = w.watcher.Add(e.Name)
			}
		}
	}
	return e, err
}

// Add a file to the watcher
func (w *FileWatcher) Add(name string) error {
	if err := w.watcher.Add(name); err != nil {
		return err
	}
	w.added[name] = true // Explicitly added, don't auto-Remove

	// Scan directories for existing symlinks, we wont' get a Create for those.
	if infos, err := ioutil.ReadDir(name); err == nil {
		for _, info := range infos {
			if isSymlink(info) {
				debug("Is a symlink")
				_ = w.watcher.Add(filepath.Join(name, info.Name()))
			}
		}
	}
	return nil
}

// Remove name from watcher
func (w *FileWatcher) Remove(name string) error {
	delete(w.added, name)
	return w.watcher.Remove(name)
}

// Close watcher
func (w *FileWatcher) Close() error { return w.watcher.Close() }

func isSymlink(info os.FileInfo) bool {
	return (info.Mode() & os.ModeSymlink) == os.ModeSymlink
}
//////////////////////



type FileWatcher struct {
	watcher *fsnotify.Watcher
	metrics *prometheus.CounterVec
	sizes   map[string]float64
	added map[string]bool
}


func NewFileWatcher(dir string) (*FileWatcher, error) {
	var namespace string
	var podname string
	var containerid string
	var completepathoffile string

	debug("watching %v", dir)
	w := &FileWatcher{
		metrics: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "fluentd_input_status_total_bytes_logged",
			Help: "total bytes logged to disk (log file) ",
		}, []string{"path"}),
		sizes: make(map[string]float64),
		added: make(map[string]bool),
	}
	defer prometheus.Register(w.metrics)
	var err error
	if w.watcher, err = fsnotify.NewWatcher(); err == nil {
		//defer w.watcher.Close()
		err = w.Add(dir)
	}
	w.added[dir] = true

	if err != nil {
		return nil, err
	}

	// Collect existing files, after starting watcher to avoid missing any.
	// It's OK if we update the same file twice.
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		filename := file.Name()
                if (strings.Contains(filename,".log")) {
		filenameslice := strings.Split(filename,"_")
		if (len(filenameslice) == 3) {
		namespace = filenameslice[0]
		podname = filenameslice[1]
		containerid = strings.Trim(filenameslice[2],".log")
		} else {
                namespace="unknown"
		podname="unknown"
		containerid = "unknown"
		}


		debug("namespace = %v podname = %v containerid = %v",namespace, podname, containerid)
		
		completepathoffile=filepath.Join(dir, file.Name())
		rawPath, err := filepath.EvalSymlinks(completepathoffile)
		if (err == nil) {
		err := w.Update(completepathoffile)
		debug("simpathname = %v rawpathname = %v",filename, rawPath)
		if err != nil {
			debug("error in update: %v", err)
		}
	}
	}
     }
	return w, nil
}

func (w *FileWatcher) Update(path string) error {
	counter, err := w.metrics.GetMetricWithLabelValues(path)
	if err != nil {
		return err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return nil // Ignore directories
	}
	lastSize, size := float64(w.sizes[path]), float64(stat.Size())
	w.sizes[path] = size
	var add float64
	if size > lastSize {
		// File has grown, add the difference to the counter.
		add = size - lastSize
	} else if size < lastSize {
		// File truncated, starting over. Add the size.
		add = size
	}
	debug("%v: (%v->%v) +%v", path, lastSize, size, add)
	counter.Add(add)
	return nil
}

func (w FileWatcher) Watch() {
	
	for {
		e, err := w.Event()
		fatal(err)
		log.Print(e)
		w.Update(e.Name)
	}
}

func main() {
        var dir string
        var listeningport string

	flag.StringVar(&dir, "logfilespathname", "/var/log/containers/", "Give the dirname where logfiles are going to be located")
	flag.BoolVar(&debugOn, "debug", false, "Give debug option false or true")
	flag.StringVar(&listeningport, "listeningport", ":2112", "Give the listening port address where metrics can be exposed to and listened by a running prometheus server")
	flag.Parse()

	debug("logfilespathname= %v",dir)
	debug("debug option= %v",debugOn)
	debug("listening port address= %v",listeningport)


	w, err := NewFileWatcher(dir)
	if err != nil {
		debug("NewFileWatcher error")
		log.Fatal(err)
	}
	go w.Watch()
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(listeningport, nil)
}
