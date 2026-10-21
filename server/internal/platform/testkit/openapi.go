package testkit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// contractPath is the API contract, relative to the repository root.
const contractPath = "api/openapi.yaml"

type contract struct {
	path   string
	router routers.Router
}

var (
	contractOnce sync.Once
	loaded       *contract
	errContract  error
)

// loadContract parses and checks api/openapi.yaml once per test binary.
func loadContract() (*contract, error) {
	contractOnce.Do(func() { loaded, errContract = readContract() })
	return loaded, errContract
}

func readContract() (*contract, error) {
	path, err := findContract()
	if err != nil {
		return nil, err
	}
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	if err = doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("%s is not a valid OpenAPI document: %w", path, err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		return nil, fmt.Errorf("route %s: %w", path, err)
	}
	return &contract{path: path, router: router}, nil
}

// findContract walks up from the working directory (the package directory
// under go test) to the directory that holds api/openapi.yaml.
func findContract() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		p := filepath.Join(dir, filepath.FromSlash(contractPath))
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("testkit: " + contractPath + " not found above the working directory")
		}
		dir = parent
	}
}

// validate checks one exchange against the contract: the operation must
// exist, and the status, Content-Type and body must match what it declares.
func (c *contract) validate(req *http.Request, status int, header http.Header, body []byte) error {
	route, params, err := c.router.FindRoute(req)
	if err != nil {
		return fmt.Errorf("%s %s is not in %s: %w", req.Method, req.URL.Path, contractPath, err)
	}
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: params,
			Route:      route,
		},
		Status: status,
		Header: header,
		Body:   io.NopCloser(bytes.NewReader(body)),
		Options: &openapi3filter.Options{
			IncludeResponseStatus: true,
			MultiError:            true,
		},
	}
	if err := openapi3filter.ValidateResponse(context.Background(), in); err != nil {
		return fmt.Errorf("%s %s -> %d does not match %s: %w", req.Method, req.URL.Path, status, contractPath, err)
	}
	return nil
}
