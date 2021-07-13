package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

var listenAddress = flag.String("listen", ":8080", "address to listen on")

func usage() {
	log.Println("usage: userreg [flags]")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) != 0 {
		usage()
	}

	http.HandleFunc("/registeruser", registerUser)

	log.Printf("listening on %s", *listenAddress)
	log.Fatalln(http.ListenAndServe(*listenAddress, nil))
}

func registerUser(w http.ResponseWriter, r *http.Request) {
	userID := r.FormValue("user_id")

	oidcClaims := map[string]interface{}{}
	claims := r.FormValue("oidc_claims")
	if claims != "" {
		if err := json.Unmarshal([]byte(claims), &oidcClaims); err != nil {
			log.Printf("unmarshal claims: %v", err)
		}
	}

	log.Printf("/registeruser, user_id %v, oidc_claims %v", userID, oidcClaims)

	// todo: lookup some field in the claims, and match it with organization in chirpstack?

	fmt.Fprintln(w, "ok")
}
