package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("symbol") != "AAPL" {
			t.Errorf("symbol query = %q", request.URL.Query().Get("symbol"))
		}
		if request.URL.Query().Get("apikey") != "secret" {
			t.Errorf("API key query was not passed")
		}
		fmt.Fprint(writer, `[{"symbol":"AAPL","price":200,"dayLow":195,"dayHigh":205,"volume":1234567}]`)
	}))
	defer server.Close()

	sim := &MonteCarlo{}
	err := sim.fetchAPIWithClient(context.Background(), " aapl ", "secret", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if sim.StockSymbol != "AAPL" || sim.UnderlyingPrice != 200 || sim.Volume != 1234567 {
		t.Fatalf("unexpected quote mapping: %+v", sim)
	}
	if sim.Volatility <= 0 || sim.VolatilitySource == "" {
		t.Fatalf("volatility was not populated: %+v", sim)
	}
}

func TestFetchAPIEmptyResponseNamesSymbol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fmt.Fprint(writer, `[]`)
	}))
	defer server.Close()

	err := (&MonteCarlo{}).fetchAPIWithClient(context.Background(), "AAPL", "secret", server.URL, server.Client())
	if err == nil || !strings.Contains(err.Error(), "AAPL") || strings.Contains(err.Error(), "<nil>") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchAPIDoesNotExposeKeyInStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	err := (&MonteCarlo{}).fetchAPIWithClient(context.Background(), "AAPL", "top-secret", server.URL, server.Client())
	if err == nil {
		t.Fatal("expected HTTP status error")
	}
	if strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("error exposed API key: %v", err)
	}
}

func TestFetchAPIFallsBackWhenRangeIsInvalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fmt.Fprint(writer, `[{"symbol":"AAPL","price":200,"dayLow":0,"dayHigh":0}]`)
	}))
	defer server.Close()

	sim := &MonteCarlo{}
	if err := sim.fetchAPIWithClient(context.Background(), "AAPL", "secret", server.URL, server.Client()); err != nil {
		t.Fatal(err)
	}
	if sim.Volatility != 0.25 || !strings.Contains(sim.VolatilitySource, "fallback") {
		t.Fatalf("expected documented fallback, got %+v", sim)
	}
}

func TestStepsForDays(t *testing.T) {
	tests := []struct {
		days float64
		want int
	}{
		{days: 0, want: 1},
		{days: 7, want: 5},
		{days: 30, want: 21},
		{days: 365, want: 252},
	}
	for _, testCase := range tests {
		if got := stepsForDays(testCase.days); got != testCase.want {
			t.Fatalf("stepsForDays(%v) = %d, want %d", testCase.days, got, testCase.want)
		}
	}
}
