package shared

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
)

func init() {
	godotenv.Load()
}

// go to repo root, leaving /scripts
func GoToRootDir() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	if strings.HasSuffix(wd, "scripts") || strings.HasSuffix(wd, "scripts/") {
		// go to root, or fail
		err := os.Chdir("..")
		if err != nil {
			panic(err)
		}
	}
}
