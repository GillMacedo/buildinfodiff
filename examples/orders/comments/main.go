// Command orders is a small made-up service used to show buildinfodiff.
package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// version is set at build time with -ldflags -X main.version=...
var version = "dev"

func main() {
	// Item names are shown in title case.
	title := cases.Title(language.English)
	http.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s %s\n", uuid.NewString(), title.String(r.URL.Query().Get("item")))
	})
	http.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, version)
	})
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	http.ListenAndServe("127.0.0.1:8080", nil)
}
