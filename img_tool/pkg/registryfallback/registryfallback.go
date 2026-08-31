package registryfallback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// Candidates parses a comma-separated registry list, preserving its order.
func Candidates(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	registries := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		registry := strings.TrimSpace(part)
		if registry == "" {
			return nil, fmt.Errorf("registry list contains an empty entry: %q", value)
		}
		if _, ok := seen[registry]; ok {
			continue
		}
		seen[registry] = struct{}{}
		registries = append(registries, registry)
	}
	return registries, nil
}

// Do tries registries in order. It advances only for failures that indicate
// registry unavailability, throttling, or missing/incorrect authentication.
func Do[T any](ctx context.Context, value string, attempt func(string) (T, error)) (T, string, error) {
	var zero T
	registries, err := Candidates(value)
	if err != nil {
		return zero, "", err
	}
	for i, registry := range registries {
		if err := ctx.Err(); err != nil {
			return zero, "", err
		}
		result, err := attempt(registry)
		if err == nil {
			return result, registry, nil
		}
		if i == len(registries)-1 || !ShouldTryNext(err) {
			return zero, registry, err
		}
		log.Printf("registry %s failed with an eligible failover error; trying %s: %v", registry, registries[i+1], err)
	}
	return zero, "", errors.New("registry list is empty")
}

// ShouldTryNext reports whether err is safe to treat as specific to the
// current registry. Client/policy failures deliberately return false.
func ShouldTryNext(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var registryErr *transport.Error
	if errors.As(err, &registryErr) {
		if registryErr.StatusCode == http.StatusUnauthorized ||
			registryErr.StatusCode == http.StatusRequestTimeout ||
			registryErr.StatusCode == http.StatusTooManyRequests ||
			registryErr.StatusCode >= http.StatusInternalServerError {
			return true
		}
		if len(registryErr.Errors) == 0 {
			return false
		}
		for _, diagnostic := range registryErr.Errors {
			switch diagnostic.Code {
			case transport.UnauthorizedErrorCode,
				transport.TooManyRequestsErrorCode,
				transport.UnavailableErrorCode:
			default:
				return false
			}
		}
		return true
	}

	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		return true
	}
	var temporary interface{ Temporary() bool }
	return errors.As(err, &temporary) && temporary.Temporary()
}
