package main

import (
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
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

type FileWatcher struct {
	watcher *fsnotify.Watcher
	metrics *prometheus.CounterVec
	sizes   map[string]float64
}


func NewFileWatcher(dir string) (*FileWatcher, error) {
	var namespace string
	var podname string
	var containerid string

	debug("watching %v", dir)
	w := &FileWatcher{
		metrics: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "fluentd_input_status_total_bytes_logged",
			Help: "total bytes logged to disk (log file) ",
		}, []string{"path"}),
		sizes: make(map[string]float64),
	}
	defer prometheus.Register(w.metrics)
	var err error
	if w.watcher, err = fsnotify.NewWatcher(); err == nil {
		err = w.watcher.Add(dir)
	}
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
		err := w.Update(filepath.Join(dir, file.Name()))
		if err != nil {
			debug("error in update: %v", err)
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
		var err error
		select {
		case e := <-w.watcher.Events:
			debug("event %v", e)
			if e.Op == fsnotify.Write {
				// Write (which includes truncate) is the only operation that can change file size.
				w.Update(e.Name)
			}
			if err != nil && !os.IsNotExist(err) {
				debug("watch error: %v", err)
			}
		case err = <-w.watcher.Errors:
			if err != nil {
				debug("watch error: %v", err)
			}
		}
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
		log.Fatal(err)
	}
	go w.Watch()
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(listeningport, nil)
}
