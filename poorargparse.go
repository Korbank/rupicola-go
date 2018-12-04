package rupicola

// The expected high quality Just-Work (tm)

import (
	"os"
)

var (
	poorStringStorage = make(map[string]*string)
)

func poorString(name string, def string, desc string) *string {
	s := new(string)
	poorStringStorage[name] = s
	*s = def
	return s
}

func poorParse() {
	if len(os.Args) > 2 && os.Args[1] == "--config" {
		*poorStringStorage["config"] = os.Args[2]
	}
}

func poorUsage() {
	println("Usage")
}
