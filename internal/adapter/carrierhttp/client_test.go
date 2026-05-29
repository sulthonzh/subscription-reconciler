package carrierhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckPlan_Active(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "u_42", r.URL.Query().Get("userId"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"active"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	status, err := client.CheckPlan(context.Background(), "u_42")

	assert.NoError(t, err)
	assert.Equal(t, "active", status)
}

func TestCheckPlan_Inactive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"inactive"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	status, err := client.CheckPlan(context.Background(), "u_42")

	assert.NoError(t, err)
	assert.Equal(t, "inactive", status)
}

func TestCheckPlan_ApiError503(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	status, err := client.CheckPlan(context.Background(), "u_42")

	assert.NoError(t, err)
	assert.Equal(t, "api_error", status)
}

func TestCheckPlan_NetworkError(t *testing.T) {
	client := NewClient("http://127.0.0.1:0")
	status, err := client.CheckPlan(context.Background(), "u_42")

	assert.NoError(t, err)
	assert.Equal(t, "api_error", status)
}

func TestCheckPlan_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	status, err := client.CheckPlan(context.Background(), "u_42")

	assert.NoError(t, err)
	assert.Equal(t, "api_error", status)
}

func TestCheckPlan_CancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"active"}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewClient(server.URL)
	status, err := client.CheckPlan(ctx, "u_42")

	assert.NoError(t, err)
	assert.Equal(t, "api_error", status)
}

func TestCheckPlan_Status403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	status, err := client.CheckPlan(context.Background(), "u_42")

	assert.NoError(t, err)
	assert.Equal(t, "api_error", status)
}

func TestCheckPlan_Status404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	status, err := client.CheckPlan(context.Background(), "u_42")

	assert.NoError(t, err)
	assert.Equal(t, "api_error", status)
}

func TestCheckPlan_Status500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	status, err := client.CheckPlan(context.Background(), "u_42")

	assert.NoError(t, err)
	assert.Equal(t, "api_error", status)
}

func TestCheckPlan_NetworkErrorInRequestCreation(t *testing.T) {
	client := NewClient("://invalid-url")
	status, err := client.CheckPlan(context.Background(), "u_42")

	assert.NoError(t, err)
	assert.Equal(t, "api_error", status)
}
