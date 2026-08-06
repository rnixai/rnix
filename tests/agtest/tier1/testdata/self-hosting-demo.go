// Planted fixture for Story 49.1 (NR-1 self-hosting evidence). Known defects intentionally introduced.

package main

import (
	"fmt"
	"os"
)

// splitUsers splits users into adjacent pairs.
// [Critical] 越界: users[i+1] panics when i == len(users)-1 (no bounds check).
func splitUsers(users []string) [][2]string {
	var pairs [][2]string
	for i := 0; i < len(users); i++ {
		pairs = append(pairs, [2]string{users[i], users[i+1]})
	}
	return pairs
}

// loadConfig reads the config file.
// [Warning] 未检查错误: the error from os.ReadFile is discarded.
func loadConfig(cfgPath string) []byte {
	data, _ := os.ReadFile(cfgPath)
	return data
}

// openLog opens a log file for appending.
// [Warning] 资源泄漏: the file handle is never closed (no defer f.Close()).
func openLog(logPath string) (*os.File, error) {
	f, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func main() {
	users := []string{"alice", "bob"}
	_ = splitUsers(users)
	_ = loadConfig("app.conf")
	if f, err := openLog("app.log"); err == nil {
		_ = f
	}
	fmt.Println("done")
}
