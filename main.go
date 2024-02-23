package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"github.com/mjl-/sconf"
	"google.golang.org/grpc"
)

var listenAddress = flag.String("listen", ":8080", "address to listen on")

var config struct {
	Chirpstack struct {
		Address string `sconf-doc:"Address in host:port format for connecting to chirpstack, eg chirpstack:8080."`
		ApiKey  string `sconf-doc:"ChirpStack API Key."`
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
	var oidcClaims struct {
		SchacHomeOrganization  string   `json:"schac_home_organization"`
		EdupersonAffiliation   []string `json:"eduperson_affiliation"`
		EdupersonPrincipalName string   `json:"eduperson_principal_name"`
	}

	userID := r.FormValue("user_id")
	err := json.NewDecoder(r.Body).Decode(&oidcClaims)
	if err != nil {
		httpUserErrorf(w, "decode oidc claims error: %s", err)
		return
	}

	log.Printf("/registeruser, user_id %v, oidc_claims %s", userID, oidcClaims)

	if userID == "" {
		httpUserErrorf(w, "missing user_id parameter")
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

	tenantClient := api.NewTenantServiceClient(conn)

	listResp, err := tenantClient.List(r.Context(), &api.ListTenantsRequest{Limit: 10000})
	if err != nil {
		httpServerErrorf(w, "listing organizations: %v", err)
		return
	}
	var destTenant *api.TenantListItem
	for _, t := range listResp.Result {
		if t.Name == oidcClaims.SchacHomeOrganization {
			destTenant = t
			break
		}
	}
	if destTenant == nil {
		req := &api.CreateTenantRequest{
			Tenant: &api.Tenant{
				Name:            oidcClaims.SchacHomeOrganization,
				CanHaveGateways: true,
			},
		}
		resp, err := tenantClient.Create(r.Context(), req)
		if err != nil {
			httpServerErrorf(w, "creating tenant %q in chirpstack: %v", oidcClaims.SchacHomeOrganization, err)
			return
		}
		destTenant = &api.TenantListItem{
			Id:   resp.Id,
			Name: req.Tenant.Name,
		}
	}

	userClient := api.NewUserServiceClient(conn)
	userResp, err := userClient.Get(r.Context(), &api.GetUserRequest{Id: userID})
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

	addUserReq := &api.AddTenantUserRequest{
		TenantUser: &api.TenantUser{
			TenantId: destTenant.Id,
			Email:    userResp.User.Email,
		},
	}
	if _, err := tenantClient.AddUser(r.Context(), addUserReq); err != nil {
		httpServerErrorf(w, "adding user to tenant: %v", err)
		return
	}

	fmt.Fprintf(w, "user %d added to tenant %q (%d)\n", userID, &destTenant.Name, destTenant.Id)
}

type apitoken struct{}

func (a apitoken) GetRequestMetadata(ctx context.Context, url ...string) (map[string]string, error) {
	if config.Chirpstack.ApiKey == "" {
		return map[string]string{}, nil
	}
	return map[string]string{
		"authorization": fmt.Sprintf("Bearer %s", config.Chirpstack.ApiKey),
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
