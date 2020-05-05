# GCG1

Java garbage collection metrics.  

## Install

Edit and copy the `gcg1.yaml` file to the appropriate folder.  This is a v2 plugin that only works with v3 snap.

## Setup

For Java 8 you need to set the following options.

```
-XX:+UseG1GC -verbose:gc -XX:+PrintGCDetails -XX:+PrintGCDateStamps -XX:+PrintGCTimeStamps -Xloggc:/location/of/gc.log
```

For Java 9+

```
coming soon
```


## Testing

testing manually:

```
go run main.go -debug-mode=1 -plugin-config='{"groups": [{"type":"java_g1", "locations": ["/workspaces/garbage-collector/gc.log"]}]}' -debug-collect-counts=100 -debug-collect-interval=10s
```


run test suite
```
go test collector/*
```