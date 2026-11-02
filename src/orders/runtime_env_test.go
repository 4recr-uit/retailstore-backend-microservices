package main

import (
	"context"
	"fmt"
	appconfig "github.com/aws-containers/retail-store-sample-app/orders/pkg/config"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// Emulate the SDK container-credential endpoint; no AWS request is made.
func TestSQSUsesRuntimeContainerCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "local-token" {
			t.Error("missing credential authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"AccessKeyId":"runtime-pod-key","SecretAccessKey":"runtime-pod-secret","Token":"runtime-session","Expiration":%q}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	}))
	defer server.Close()
	for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE", "AWS_ROLE_ARN", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE"} {
		t.Setenv(key, "")
	}
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", server.URL)
	t.Setenv("AWS_CONTAINER_AUTHORIZATION_TOKEN", "local-token")
	client, err := initSQSClient(context.Background(), &appconfig.Config{AWSRegion: "us-west-2"})
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := client.Options().Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessKeyID != "runtime-pod-key" {
		t.Fatalf("SDK runtime credentials were replaced: %s", credentials.AccessKeyID)
	}
}
