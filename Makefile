all: build

copy_files: 
	cp -r ./src Docker/.

build:  copy_files
	hack/build-component-image.sh Docker openshift/go-log-file-watcher-driver-v0
