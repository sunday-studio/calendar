package telemetry_test

import "net/http"

type httpGet struct{}

func (httpGet) do(url string) (*http.Response, error) { return http.Get(url) }
