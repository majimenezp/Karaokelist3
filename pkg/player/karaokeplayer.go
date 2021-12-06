package player

import (
	"Karaokelist3/pkg/entities"
	"Karaokelist3/pkg/website"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
)

func StartPlayer(args entities.RunParams) {
	fmt.Fprintln(os.Stdout, "Starting karaokelist")
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	router := mux.NewRouter()
	router.HandleFunc("/", website.MainHandler)
	router.HandleFunc("/admin", website.AdminHandler)
	router.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		website.SearchPostHandler(w, r, args)
	}).Methods("POST")
	router.HandleFunc("/startindexing", func(w http.ResponseWriter, r *http.Request) {
		website.StartIndenxingHandler(w, r, args)
	}).Methods("POST")

	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./web/static"))))
	website.WebsiteInit()
	srv := &http.Server{
		Handler: router,
		Addr:    ":8080",
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Fatal(srv.ListenAndServe())
	//cdgLoader := cdgloader.CDGLoader{}
	//loaded, err := cdgLoader.LoadFile("/mnt/d/Projects/AcDc - Shoot To Thrill.cdg")

	// if err == nil {
	// 	fmt.Fprintln(os.Stdout, "loaded "+strconv.FormatBool(loaded))

	// }
}
