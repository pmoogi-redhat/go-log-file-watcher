FROM openshift/origin-release:golang-1.15 AS build
RUN mkdir -p /go-log-file-watcher/src
COPY ./src/log-file-watcher.go /go-log-file-watcher/src/.
WORKDIR /go-log-file-watcher 
RUN go get -d -v ./...
EXPOSE 8080
RUN go build -o log-file-watcher ./src/log-file-watcher.go
ENV pathoflogfiles=/var/log/containers/
ENV debugOn=true
ENV port=:2112
CMD echo "./log-file-watcher -logfilespathname=${pathoflogfiles} -debug=${debugOn} -listeningport=${port}"
CMD ./log-file-watcher -logfilespathname=${pathoflogfiles} -debug=${debugOn} -listeningport=${port}
