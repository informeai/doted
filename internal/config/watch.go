package config

import (
	"os"
	"time"
)

// Watch polls path every interval and calls onChange whenever the file is
// created, modified or removed. It blocks forever; run it in a goroutine.
func Watch(path string, interval time.Duration, onChange func()) {
	last := stamp(path)
	for range time.Tick(interval) {
		if s := stamp(path); s != last {
			last = s
			onChange()
		}
	}
}

type fileStamp struct {
	exists  bool
	size    int64
	modTime time.Time
}

func stamp(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{exists: true, size: info.Size(), modTime: info.ModTime()}
}
