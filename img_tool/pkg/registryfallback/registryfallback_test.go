package registryfallback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

func TestCandidates(t *testing.T) {
	got, err := Candidates(" first.example,second.example, first.example ")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first.example", "second.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Candidates() = %v, want %v", got, want)
	}
	if _, err := Candidates("first.example,,second.example"); err == nil {
		t.Fatal("Candidates() accepted an empty entry")
	}
}

func TestDoTriesEligibleFailuresInOrder(t *testing.T) {
	var attempted []string
	result, selected, err := Do(context.Background(), "one.example,two.example,three.example", func(registry string) (string, error) {
		attempted = append(attempted, registry)
		switch registry {
		case "one.example":
			return "", &transport.Error{StatusCode: http.StatusTooManyRequests}
		case "two.example":
			return "", fmt.Errorf("upload: %w", io.ErrUnexpectedEOF)
		default:
			return "ok", nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" || selected != "three.example" {
		t.Fatalf("Do() = (%q, %q), want (ok, three.example)", result, selected)
	}
	wantAttempted := []string{"one.example", "two.example", "three.example"}
	if !reflect.DeepEqual(attempted, wantAttempted) {
		t.Fatalf("attempted %v, want %v", attempted, wantAttempted)
	}
}

func TestDoStopsOnClientOrPolicyFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "denied", err: &transport.Error{StatusCode: http.StatusForbidden, Errors: []transport.Diagnostic{{Code: transport.DeniedErrorCode}}}},
		{name: "invalid manifest", err: &transport.Error{StatusCode: http.StatusBadRequest, Errors: []transport.Diagnostic{{Code: transport.ManifestInvalidErrorCode}}}},
		{name: "local error", err: errors.New("invalid image configuration")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 0
			_, selected, err := Do(context.Background(), "one.example,two.example", func(string) (struct{}, error) {
				attempts++
				return struct{}{}, tc.err
			})
			if !errors.Is(err, tc.err) {
				t.Fatalf("Do() error = %v, want %v", err, tc.err)
			}
			if attempts != 1 || selected != "one.example" {
				t.Fatalf("Do() made %d attempts and selected %q, want one attempt on one.example", attempts, selected)
			}
		})
	}
}

func TestShouldTryNextRegistryErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "unauthorized status", err: &transport.Error{StatusCode: http.StatusUnauthorized}, want: true},
		{name: "unauthorized diagnostic", err: &transport.Error{StatusCode: http.StatusForbidden, Errors: []transport.Diagnostic{{Code: transport.UnauthorizedErrorCode}}}, want: true},
		{name: "server error", err: &transport.Error{StatusCode: http.StatusInternalServerError}, want: true},
		{name: "unavailable", err: &transport.Error{StatusCode: http.StatusBadRequest, Errors: []transport.Diagnostic{{Code: transport.UnavailableErrorCode}}}, want: true},
		{name: "mixed diagnostics", err: &transport.Error{StatusCode: http.StatusBadRequest, Errors: []transport.Diagnostic{{Code: transport.UnavailableErrorCode}, {Code: transport.ManifestInvalidErrorCode}}}, want: false},
		{name: "denied", err: &transport.Error{StatusCode: http.StatusForbidden, Errors: []transport.Diagnostic{{Code: transport.DeniedErrorCode}}}, want: false},
		{name: "canceled", err: context.Canceled, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldTryNext(tc.err); got != tc.want {
				t.Fatalf("ShouldTryNext() = %v, want %v", got, tc.want)
			}
		})
	}
}
