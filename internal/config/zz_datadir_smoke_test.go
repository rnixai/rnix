package config

import (
	"fmt"
	"testing"
)

func TestZZ_DataDirSmoke(t *testing.T) {
	t.Setenv("RNIX_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	dd, _ := DataDir()
	fmt.Println("DataDir:", dd)
	fmt.Println("Project A (echomatrix):", ProjectDataDir(dd, "/mnt/disk0/project/echomatrix"))
	fmt.Println("Project B (CJK+spaces+!):", ProjectDataDir(dd, "/home/user/我的 项目!"))
	fmt.Println("Empty proj:", ProjectDataDir(dd, ""))
	fmt.Println("ID spaces:", ProjectDataID("/home/user/my cool project"))
	fmt.Println("ID CJK:", ProjectDataID("/home/user/我的项目"))
}
