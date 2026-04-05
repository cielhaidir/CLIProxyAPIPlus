package managementasset

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseGitHubRepo(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		owner    string
		repoName string
		wantErr  bool
	}{
		{name: "github repo url", repo: "https://github.com/example/panel", owner: "example", repoName: "panel"},
		{name: "github repo url git suffix", repo: "https://github.com/example/panel.git", owner: "example", repoName: "panel"},
		{name: "api releases url", repo: "https://api.github.com/repos/example/panel/releases/latest", owner: "example", repoName: "panel"},
		{name: "invalid", repo: "not-a-url", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repoName, err := parseGitHubRepo(tt.repo)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if owner != tt.owner || repoName != tt.repoName {
				t.Fatalf("expected %s/%s, got %s/%s", tt.owner, tt.repoName, owner, repoName)
			}
		})
	}
}

func TestRemoteAssetExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	client := server.Client()

	ok, err := remoteAssetExists(ctx, client, server.URL+"/ok")
	if err != nil || !ok {
		t.Fatalf("expected ok=true, err=nil, got ok=%v err=%v", ok, err)
	}

	ok, err = remoteAssetExists(ctx, client, server.URL+"/missing")
	if err != nil || ok {
		t.Fatalf("expected ok=false, err=nil, got ok=%v err=%v", ok, err)
	}

	if _, err = remoteAssetExists(ctx, client, server.URL+"/boom"); err == nil {
		t.Fatalf("expected error for unexpected status")
	}
}
