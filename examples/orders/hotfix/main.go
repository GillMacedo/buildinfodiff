// Command orders is a small made-up service used to show buildinfodiff.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// version is set at build time with -ldflags -X main.version=...
var version = "dev"

func main() {
	// Item names are shown in title case.
	title := cases.Title(language.English)
	var group singleflight.Group
	http.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		// Hotfix: collapse duplicate requests for the same item.
		v, _, _ := group.Do(r.URL.Query().Get("item"), func() (any, error) {
			time.Sleep(10 * time.Millisecond)
			return title.String(r.URL.Query().Get("item")), nil
		})
		fmt.Fprintf(w, "%s %s\n", uuid.NewString(), v)
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
