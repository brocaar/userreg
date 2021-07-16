package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/brocaar/chirpstack-api/go/v3/as/external/api"
	"github.com/mjl-/sconf"
	"google.golang.org/grpc"
)

var listenAddress = flag.String("listen", ":8080", "address to listen on")

var config struct {
	Chirpstack struct {
		Address  string `sconf-doc:"Address in host:port format for connecting to chirpstack, eg as:8080."`
		Username string `sconf-doc:"For logging into chirpstack."`
		Password string `sconf-doc:"For logging into chirpstack."`
	} `sconf-doc:"The chirpstack to which this controlapp is linked."`
}

func usage() {
	log.Println("usage: userreg [flags] userreg.conf")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		usage()
	}

	if err := sconf.ParseFile(args[0], &config); err != nil {
		log.Fatalf("parsing config file: %v", err)
	}

	http.HandleFunc("/registeruser", registerUser)

	log.Printf("listening on %s", *listenAddress)
	log.Fatalln(http.ListenAndServe(*listenAddress, nil))
}

func registerUser(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.FormValue("user_id")
	claims := r.FormValue("oidc_claims")
	log.Printf("/registeruser, user_id %v, oidc_claims %s", userIDStr, claims)

	if userIDStr == "" {
		httpUserErrorf(w, "missing user_id parameter")
		return
	}
	if claims == "" {
		httpUserErrorf(w, "missing oidc_claims parameter")
		return
	}

	var oidcClaims struct {
		SchacHomeOrganization  string   `json:"schac_home_organization"`
		EdupersonAffiliation   []string `json:"eduperson_affiliation"`
		EdupersonPrincipalName string   `json:"eduperson_principal_name"`
	}

	if err := json.Unmarshal([]byte(claims), &oidcClaims); err != nil {
		httpUserErrorf(w, "oidc_claims user_id parameter: %v", err)
		return
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		httpUserErrorf(w, "bad user_id parameter: %v", err)
		return
	}

	for _, affil := range oidcClaims.EdupersonAffiliation {
		if affil == "student" {
			httpUserErrorf(w, "students cannot be automatically provisioned, affiliations: %s", strings.Join(oidcClaims.EdupersonAffiliation, ", "))
			return
		}
	}

	conn, err := dial()
	if err != nil {
		httpServerErrorf(w, "dialing chirpstack: %v", err)
		return
	}
	defer conn.Close()

	orgClient := api.NewOrganizationServiceClient(conn)

	listResp, err := orgClient.List(r.Context(), &api.ListOrganizationRequest{Limit: 10000})
	if err != nil {
		httpServerErrorf(w, "listing organizations: %v", err)
		return
	}
	var destOrg *api.OrganizationListItem
	for _, org := range listResp.Result {
		if org.DisplayName == oidcClaims.SchacHomeOrganization {
			destOrg = org
			break
		}
	}
	if destOrg == nil {
		httpUserErrorf(w, "organization not present in chirpstack: %q", oidcClaims.SchacHomeOrganization)
		return
	}

	userClient := api.NewUserServiceClient(conn)
	userResp, err := userClient.Get(r.Context(), &api.GetUserRequest{Id: int64(userID)})
	if err != nil {
		httpServerErrorf(w, "fetching user from chirpstack for userID %d: %v", userID, err)
		return
	}

	user := userResp.User
	user.Note = "eduperson_principal_name: " + oidcClaims.EdupersonPrincipalName
	if _, err := userClient.Update(r.Context(), &api.UpdateUserRequest{User: user}); err != nil {
		httpServerErrorf(w, "updating user with eduperson_principal_name added to note-field: %v", err)
		return
	}

	addUserReq := &api.AddOrganizationUserRequest{
		OrganizationUser: &api.OrganizationUser{
			OrganizationId: destOrg.Id,
			Email:          userResp.User.Email,
		},
	}
	if _, err := orgClient.AddUser(r.Context(), addUserReq); err != nil {
		httpServerErrorf(w, "adding user to organization: %v", err)
		return
	}

	fmt.Fprintf(w, "user %d added to organization %q (%d)\n", userID, destOrg.DisplayName, destOrg.Id)
}

var apiToken string

type apitoken struct{}

func (a apitoken) GetRequestMetadata(ctx context.Context, url ...string) (map[string]string, error) {
	if apiToken == "" {
		return map[string]string{}, nil
	}
	return map[string]string{
		"authorization": fmt.Sprintf("Bearer %s", apiToken),
	}, nil
}

func (a apitoken) RequireTransportSecurity() bool {
	return false
}

func dial() (*grpc.ClientConn, error) {
	dialOpts := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithTimeout(5 * time.Second),
		grpc.WithPerRPCCredentials(apitoken{}),
		grpc.WithInsecure(), // remove this when using TLS
	}

	conn, err := grpc.Dial(config.Chirpstack.Address, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %v", config.Chirpstack.Address, err)
	}

	intClient := api.NewInternalServiceClient(conn)
	resp, err := intClient.Login(context.Background(), &api.LoginRequest{Email: config.Chirpstack.Username, Password: config.Chirpstack.Password})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("login: %v", err)
	}
	apiToken = resp.Jwt
	return conn, nil
}

func httpServerErrorf(w http.ResponseWriter, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("http server error: %s", msg)
	http.Error(w, msg, http.StatusInternalServerError)
}

func httpUserErrorf(w http.ResponseWriter, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("http user error: %s", msg)
	http.Error(w, msg, http.StatusBadRequest)
}
